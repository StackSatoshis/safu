package audit

import "testing"

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		max  int
		want int
	}{
		{"requests", "requests", 2, 0},
		{"requets", "requests", 2, 1},
		{"loadsh", "lodash", 2, 2},
		{"abcdef", "uvwxyz", 2, 3}, // exceeds max -> max+1
		{"", "abc", 2, 3},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b, c.max); got != c.want {
			t.Errorf("levenshtein(%q,%q,%d) = %d, want %d", c.a, c.b, c.max, got, c.want)
		}
	}
}

func TestTyposquatCheck(t *testing.T) {
	ts := DefaultTyposquat()
	cases := []struct {
		eco        Ecosystem
		name       string
		wantHit    bool
		wantTarget string
	}{
		{PyPI, "requests", false, ""}, // exact popular package
		{PyPI, "requets", true, "requests"},
		{PyPI, "nummpy", true, "numpy"},
		{NPM, "loadsh", true, "lodash"},
		{NPM, "expres", true, "express"},
		{PyPI, "abc", false, ""}, // too short
		{PyPI, "totally-unique-name", false, ""},
		{Crates, "tokio", false, ""}, // exact
		{Crates, "tokoi", true, "tokio"},
	}
	for _, c := range cases {
		hit := ts.Check(c.eco, c.name)
		if c.wantHit {
			if hit == nil {
				t.Errorf("Check(%s,%q) = nil, want hit on %q", c.eco, c.name, c.wantTarget)
				continue
			}
			if hit.Target != c.wantTarget {
				t.Errorf("Check(%s,%q) target = %q, want %q", c.eco, c.name, hit.Target, c.wantTarget)
			}
		} else if hit != nil {
			t.Errorf("Check(%s,%q) = %+v, want nil", c.eco, c.name, hit)
		}
	}
}

func TestSeverityBucket(t *testing.T) {
	mk := func(dbSpec, cvss string) osvVulnDetail {
		var d osvVulnDetail
		d.DatabaseSpecific.Severity = dbSpec
		if cvss != "" {
			d.Severity = []struct {
				Type  string `json:"type"`
				Score string `json:"score"`
			}{{Type: "CVSS_V3", Score: cvss}}
		}
		return d
	}
	cases := []struct {
		name string
		d    osvVulnDetail
		want string
	}{
		{"db-specific high", mk("HIGH", ""), "high"},
		{"db-specific critical", mk("CRITICAL", ""), "critical"},
		{"db-specific moderate", mk("MODERATE", ""), "moderate"},
		{"numeric score critical", mk("", "9.8"), "critical"},
		{"numeric score moderate", mk("", "5.0"), "moderate"},
		{"cvss vector unknown", mk("", "CVSS:3.1/AV:N/AC:L"), ""},
		{"nothing", mk("", ""), ""},
	}
	for _, c := range cases {
		if got := severityBucket(c.d); got != c.want {
			t.Errorf("%s: severityBucket = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseGitHubRepo(t *testing.T) {
	cases := []struct {
		url         string
		owner, repo string
		ok          bool
	}{
		{"https://github.com/psf/requests", "psf", "requests", true},
		{"git+https://github.com/lodash/lodash.git", "lodash", "lodash", true},
		{"git@github.com:tokio-rs/tokio.git", "tokio-rs", "tokio", true},
		{"https://gitlab.com/foo/bar", "", "", false},
		{"not a url", "", "", false},
	}
	for _, c := range cases {
		owner, repo, ok := parseGitHubRepo(c.url)
		if ok != c.ok || owner != c.owner || repo != c.repo {
			t.Errorf("parseGitHubRepo(%q) = (%q,%q,%v), want (%q,%q,%v)", c.url, owner, repo, ok, c.owner, c.repo, c.ok)
		}
	}
}

func TestNormalizeRepoURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"git+https://github.com/a/b.git", "https://github.com/a/b"},
		{"git@github.com:a/b.git", "https://github.com/a/b"},
		{"http://github.com/a/b", "https://github.com/a/b"},
		{"github.com/a/b", "https://github.com/a/b"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeRepoURL(c.in); got != c.want {
			t.Errorf("normalizeRepoURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPickRepoURL(t *testing.T) {
	got := pickRepoURL(map[string]string{
		"Documentation": "https://docs.example.com",
		"Source":        "https://github.com/psf/requests",
	}, "")
	if got != "https://github.com/psf/requests" {
		t.Errorf("pickRepoURL preferred-key = %q", got)
	}

	got = pickRepoURL(nil, "https://github.com/a/b")
	if got != "https://github.com/a/b" {
		t.Errorf("pickRepoURL homepage-fallback = %q", got)
	}

	got = pickRepoURL(map[string]string{"Homepage": "https://example.com"}, "https://example.com")
	if got != "" {
		t.Errorf("pickRepoURL no-code-host = %q, want empty", got)
	}
}
