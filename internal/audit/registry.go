package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// userAgent identifies safu to registries. It carries no user-identifying
// information — only the tool name and project URL (privacy contract §10).
const userAgent = "safu-audit/dev (+https://safu.sh)"

// errNotFound is returned when a registry reports the package does not exist
// (HTTP 404). It is distinct from a transport/5xx error: "not found" is a fact
// we can act on, not an unverified state.
var errNotFound = errors.New("package not found in registry")

// RegistryMeta is the metadata safu extracts from a package registry. The
// *Known flags distinguish "signal is genuinely zero/absent" from "this
// registry/endpoint didn't give us the number", so scoring never warns on data
// we simply couldn't fetch.
type RegistryMeta struct {
	Found          bool
	FirstPublished time.Time
	FirstKnown     bool
	LatestRelease  time.Time
	LatestKnown    bool
	LatestVersion  string // latest/newest version string, used to scope OSV when unpinned
	ReleaseCount   int
	Downloads      int64
	DownloadsKnown bool
	RepoURL        string
}

// registryClient fetches metadata for one ecosystem. One implementation per
// registry keeps each swappable and independently testable.
type registryClient interface {
	Meta(ctx context.Context, pkg Package) (RegistryMeta, error)
}

// vulnClient queries a vulnerability/malicious-package database (OSV).
type vulnClient interface {
	// QueryBatch audits a whole batch in one call and returns results keyed by
	// Package.key().
	QueryBatch(ctx context.Context, pkgs []Package) (map[string]VulnResult, error)
}

// repoClient fetches source-repository signals for a linked repo URL.
type repoClient interface {
	Signals(ctx context.Context, repoURL string) (RepoSignals, error)
}

// BaseURLs holds the endpoint roots so tests can redirect them at an
// httptest.Server. Production uses DefaultBaseURLs.
type BaseURLs struct {
	PyPI         string // e.g. https://pypi.org/pypi
	PyPIStats    string // e.g. https://pypistats.org/api
	NPM          string // e.g. https://registry.npmjs.org
	NPMDownloads string // e.g. https://api.npmjs.org/downloads
	Crates       string // e.g. https://crates.io/api/v1
	Homebrew     string // e.g. https://formulae.brew.sh/api
	OSV          string // e.g. https://api.osv.dev
	GitHub       string // e.g. https://api.github.com
}

// DefaultBaseURLs returns the real public endpoints.
func DefaultBaseURLs() BaseURLs {
	return BaseURLs{
		PyPI:         "https://pypi.org/pypi",
		PyPIStats:    "https://pypistats.org/api",
		NPM:          "https://registry.npmjs.org",
		NPMDownloads: "https://api.npmjs.org/downloads",
		Crates:       "https://crates.io/api/v1",
		Homebrew:     "https://formulae.brew.sh/api",
		OSV:          "https://api.osv.dev",
		GitHub:       "https://api.github.com",
	}
}

// getJSON performs a GET and decodes a JSON body into out. A 404 yields
// errNotFound; any other non-2xx (including 5xx and 429) yields a transport
// error so callers can fail open.
func getJSON(ctx context.Context, hc *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	return doJSON(hc, req, out)
}

// postJSON performs a POST with a JSON body and decodes the JSON response.
func postJSON(ctx context.Context, hc *http.Client, url string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	return doJSON(hc, req, out)
}

func doJSON(hc *http.Client, req *http.Request, out any) error {
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		// Drain a little of the body for context but cap it.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("%s %s: status %d: %s", req.Method, req.URL, resp.StatusCode, snippet)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", req.URL, err)
	}
	return nil
}
