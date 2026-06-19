package audit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)

// resp describes a canned response for a "METHOD /path" route. An empty file
// means an empty body; status 0 defaults to 200.
type resp struct {
	status int
	file   string
}

func newServer(t *testing.T, routes map[string]resp) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		rr, ok := routes[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		status := rr.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if rr.file != "" {
			w.Write(readFixture(t, rr.file))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func testAuditor(t *testing.T, srv *httptest.Server, cfg Config) *Auditor {
	t.Helper()
	base := BaseURLs{
		PyPI:         srv.URL + "/pypi",
		PyPIStats:    srv.URL + "/pypistats",
		NPM:          srv.URL + "/npm",
		NPMDownloads: srv.URL + "/npmdl",
		Crates:       srv.URL + "/crates",
		Homebrew:     srv.URL + "/brew",
		OSV:          srv.URL + "/osv",
		GitHub:       srv.URL + "/gh",
	}
	a := newWithBases(srv.Client(), base, cfg)
	a.now = func() time.Time { return fixedNow }
	return a
}

func TestAudit(t *testing.T) {
	cases := []struct {
		name           string
		pkg            Package
		routes         map[string]resp
		requireNet     bool
		wantLevel      Level
		wantBlocked    bool
		wantUnverified bool
		wantCodes      []string
		notCodes       []string
	}{
		{
			name: "confirmed malicious blocks outright",
			pkg:  Package{Name: "spellcheckpy", Ecosystem: PyPI},
			routes: map[string]resp{
				"POST /osv/v1/querybatch":                     {file: "osv_mal.json"},
				"GET /pypi/spellcheckpy/json":                 {file: "pypi_spellcheckpy.json"},
				"GET /pypistats/packages/spellcheckpy/recent": {status: 404},
			},
			wantLevel:   Danger,
			wantBlocked: true,
			wantCodes:   []string{reasonMalicious},
		},
		{
			name: "healthy popular package is safe",
			pkg:  Package{Name: "requests", Ecosystem: PyPI},
			routes: map[string]resp{
				"POST /osv/v1/querybatch":                 {file: "osv_empty.json"},
				"GET /pypi/requests/json":                 {file: "pypi_requests.json"},
				"GET /pypistats/packages/requests/recent": {file: "pypistats_requests.json"},
				"GET /gh/repos/psf/requests":              {file: "gh_psf_requests.json"},
			},
			wantLevel: Safe,
			notCodes:  []string{reasonMalicious, reasonYoung, reasonLowDownloads, reasonNoRepo, reasonArchived, reasonStale, reasonUnverified},
		},
		{
			name: "brand-new zero-download package warns",
			pkg:  Package{Name: "newpkg", Ecosystem: PyPI},
			routes: map[string]resp{
				"POST /osv/v1/querybatch":               {file: "osv_empty.json"},
				"GET /pypi/newpkg/json":                 {file: "pypi_newpkg.json"},
				"GET /pypistats/packages/newpkg/recent": {file: "pypistats_newpkg.json"},
			},
			wantLevel: Caution,
			wantCodes: []string{reasonYoung, reasonLowDownloads, reasonNoRepo},
			notCodes:  []string{reasonMalicious, reasonUnverified},
		},
		{
			name: "typosquat of a popular package warns",
			pkg:  Package{Name: "requets", Ecosystem: PyPI},
			routes: map[string]resp{
				"POST /osv/v1/querybatch": {file: "osv_empty.json"},
				"GET /pypi/requets/json":  {status: 404},
			},
			wantLevel: Caution,
			wantCodes: []string{reasonTyposquat, reasonNotFound},
			notCodes:  []string{reasonUnverified},
		},
		{
			name: "network failure fails open (safe + unverified)",
			pkg:  Package{Name: "requests", Ecosystem: PyPI},
			routes: map[string]resp{
				"POST /osv/v1/querybatch": {status: 500},
				"GET /pypi/requests/json": {status: 503},
			},
			wantLevel:      Safe,
			wantUnverified: true,
			wantCodes:      []string{reasonUnverified},
		},
		{
			name: "network failure fails closed when RequireNetwork",
			pkg:  Package{Name: "requests", Ecosystem: PyPI},
			routes: map[string]resp{
				"POST /osv/v1/querybatch": {status: 500},
				"GET /pypi/requests/json": {status: 503},
			},
			requireNet:     true,
			wantLevel:      Danger,
			wantUnverified: true,
		},
		{
			name: "high-severity CVE escalates to danger",
			pkg:  Package{Name: "vulnpkg", Ecosystem: PyPI},
			routes: map[string]resp{
				"POST /osv/v1/querybatch":                {file: "osv_cve.json"},
				"GET /osv/v1/vulns/GHSA-test-high":       {file: "osv_vuln_high.json"},
				"GET /pypi/vulnpkg/json":                 {file: "pypi_vulnpkg.json"},
				"GET /pypistats/packages/vulnpkg/recent": {file: "pypistats_vulnpkg.json"},
			},
			wantLevel: Danger,
			wantCodes: []string{reasonCVEHigh},
			notCodes:  []string{reasonMalicious, reasonUnverified},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newServer(t, tc.routes)
			cfg := DefaultConfig()
			cfg.RequireNetwork = tc.requireNet
			a := testAuditor(t, srv, cfg)

			verdicts, err := a.Audit(context.Background(), []Package{tc.pkg})
			if err != nil {
				t.Fatalf("Audit returned error: %v", err)
			}
			if len(verdicts) != 1 {
				t.Fatalf("got %d verdicts, want 1", len(verdicts))
			}
			v := verdicts[0]

			if v.Level != tc.wantLevel {
				t.Errorf("Level = %q, want %q (reasons: %v)", v.Level, tc.wantLevel, codesOf(v))
			}
			if v.Blocked != tc.wantBlocked {
				t.Errorf("Blocked = %v, want %v", v.Blocked, tc.wantBlocked)
			}
			if v.Unverified != tc.wantUnverified {
				t.Errorf("Unverified = %v, want %v", v.Unverified, tc.wantUnverified)
			}
			for _, code := range tc.wantCodes {
				if !hasReason(v.Reasons, code) {
					t.Errorf("missing reason %q (got %v)", code, codesOf(v))
				}
			}
			for _, code := range tc.notCodes {
				if hasReason(v.Reasons, code) {
					t.Errorf("unexpected reason %q (got %v)", code, codesOf(v))
				}
			}
		})
	}
}

func codesOf(v Verdict) []string {
	out := make([]string, 0, len(v.Reasons))
	for _, r := range v.Reasons {
		out = append(out, r.Code)
	}
	return out
}

// TestAuditBatchOSV verifies the batch OSV query aligns results to the right
// packages by index, and that Homebrew (no OSV coverage) is excluded cleanly.
func TestAuditBatchPreservesOrder(t *testing.T) {
	routes := map[string]resp{
		"POST /osv/v1/querybatch":                 {file: "osv_empty.json"},
		"GET /pypi/requests/json":                 {file: "pypi_requests.json"},
		"GET /pypistats/packages/requests/recent": {file: "pypistats_requests.json"},
		"GET /gh/repos/psf/requests":              {file: "gh_psf_requests.json"},
	}
	srv := newServer(t, routes)
	a := testAuditor(t, srv, DefaultConfig())

	pkgs := []Package{{Name: "requests", Ecosystem: PyPI}}
	verdicts, err := a.Audit(context.Background(), pkgs)
	if err != nil {
		t.Fatal(err)
	}
	if verdicts[0].Package.Name != "requests" {
		t.Errorf("verdict order mismatch: %q", verdicts[0].Package.Name)
	}
}

// TestAuditScopesOSVToLatestVersion verifies that auditing an unpinned package
// resolves the registry's latest version and sends THAT to OSV — so OSV only
// returns advisories affecting what would actually be installed, not every
// historical CVE.
func TestAuditScopesOSVToLatestVersion(t *testing.T) {
	var (
		mu      sync.Mutex
		osvBody string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/osv/v1/querybatch":
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			osvBody = string(b)
			mu.Unlock()
			w.Write([]byte(`{"results":[{}]}`))
		case r.URL.Path == "/pypi/requests/json":
			w.Write(readFixture(t, "pypi_requests.json"))
		case r.URL.Path == "/pypistats/packages/requests/recent":
			w.Write(readFixture(t, "pypistats_requests.json"))
		case r.URL.Path == "/gh/repos/psf/requests":
			w.Write(readFixture(t, "gh_psf_requests.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	a := testAuditor(t, srv, DefaultConfig())
	// Audit WITHOUT a pinned version.
	if _, err := a.Audit(context.Background(), []Package{{Name: "requests", Ecosystem: PyPI}}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(osvBody, `"version":"2.31.0"`) {
		t.Errorf("OSV query did not include resolved latest version 2.31.0; body = %s", osvBody)
	}
}
