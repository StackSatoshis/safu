package audit

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// cratesClient reads crate metadata from the crates.io API. A single endpoint
// carries age, release count, total downloads, and the repository link.
type cratesClient struct {
	hc   *http.Client
	base string // https://crates.io/api/v1
}

type cratesResponse struct {
	Crate struct {
		CreatedAt     time.Time `json:"created_at"`
		UpdatedAt     time.Time `json:"updated_at"`
		Downloads     int64     `json:"downloads"`
		NewestVersion string    `json:"newest_version"`
		Repository    string    `json:"repository"`
		Homepage      string    `json:"homepage"`
	} `json:"crate"`
	Versions []struct {
		Num string `json:"num"`
	} `json:"versions"`
}

func (c *cratesClient) Meta(ctx context.Context, pkg Package) (RegistryMeta, error) {
	var resp cratesResponse
	u := c.base + "/crates/" + url.PathEscape(pkg.Name)
	if err := getJSON(ctx, c.hc, u, &resp); err != nil {
		return RegistryMeta{}, err
	}

	m := RegistryMeta{
		Found:         true,
		ReleaseCount:  len(resp.Versions),
		LatestVersion: resp.Crate.NewestVersion,
		Downloads:     resp.Crate.Downloads,
		// crates.io always returns a total-download count.
		DownloadsKnown: true,
	}
	if !resp.Crate.CreatedAt.IsZero() {
		m.FirstPublished, m.FirstKnown = resp.Crate.CreatedAt, true
	}
	if !resp.Crate.UpdatedAt.IsZero() {
		m.LatestRelease, m.LatestKnown = resp.Crate.UpdatedAt, true
	}

	if u := normalizeRepoURL(resp.Crate.Repository); isCodeHost(u) {
		m.RepoURL = u
	} else if isCodeHost(resp.Crate.Homepage) {
		m.RepoURL = resp.Crate.Homepage
	}

	return m, nil
}
