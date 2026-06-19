package audit

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pypiClient reads package metadata from the PyPI JSON API, and recent
// download counts from pypistats (both keyless, registry-adjacent).
type pypiClient struct {
	hc        *http.Client
	base      string // .../pypi
	statsBase string // .../api  (pypistats)
}

type pypiResponse struct {
	Info struct {
		Version     string            `json:"version"`
		HomePage    string            `json:"home_page"`
		ProjectURLs map[string]string `json:"project_urls"`
	} `json:"info"`
	Releases map[string][]struct {
		UploadTime time.Time `json:"upload_time_iso_8601"`
	} `json:"releases"`
}

type pypiStatsResponse struct {
	Data struct {
		LastMonth int64 `json:"last_month"`
	} `json:"data"`
}

func (c *pypiClient) Meta(ctx context.Context, pkg Package) (RegistryMeta, error) {
	var resp pypiResponse
	u := c.base + "/" + url.PathEscape(pkg.Name) + "/json"
	if err := getJSON(ctx, c.hc, u, &resp); err != nil {
		return RegistryMeta{}, err
	}

	m := RegistryMeta{Found: true, ReleaseCount: len(resp.Releases), LatestVersion: resp.Info.Version}

	// Earliest and latest upload times across all release files.
	for _, files := range resp.Releases {
		for _, f := range files {
			if f.UploadTime.IsZero() {
				continue
			}
			if !m.FirstKnown || f.UploadTime.Before(m.FirstPublished) {
				m.FirstPublished, m.FirstKnown = f.UploadTime, true
			}
			if !m.LatestKnown || f.UploadTime.After(m.LatestRelease) {
				m.LatestRelease, m.LatestKnown = f.UploadTime, true
			}
		}
	}

	m.RepoURL = pickRepoURL(resp.Info.ProjectURLs, resp.Info.HomePage)

	// Downloads come from a separate pypistats endpoint; a failure there is a
	// per-signal gap, not a package-level failure.
	var stats pypiStatsResponse
	su := c.statsBase + "/packages/" + url.PathEscape(pkg.Name) + "/recent"
	if err := getJSON(ctx, c.hc, su, &stats); err == nil {
		m.Downloads, m.DownloadsKnown = stats.Data.LastMonth, true
	}

	return m, nil
}

// pickRepoURL chooses the most likely source-repo URL from a set of registry
// "project URLs" plus a homepage fallback, preferring entries whose key or
// value points at a known code host.
func pickRepoURL(projectURLs map[string]string, homepage string) string {
	// Prefer keys that explicitly name source/repository/code.
	preferredKeys := []string{"source", "repository", "repo", "code", "github"}
	for _, want := range preferredKeys {
		for k, v := range projectURLs {
			if strings.Contains(strings.ToLower(k), want) && isCodeHost(v) {
				return v
			}
		}
	}
	// Otherwise any project URL that points at a code host.
	for _, v := range projectURLs {
		if isCodeHost(v) {
			return v
		}
	}
	if isCodeHost(homepage) {
		return homepage
	}
	return ""
}

func isCodeHost(u string) bool {
	u = strings.ToLower(u)
	return strings.Contains(u, "github.com/") ||
		strings.Contains(u, "gitlab.com/") ||
		strings.Contains(u, "bitbucket.org/")
}
