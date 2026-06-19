package audit

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// npmClient reads package metadata from the npm registry and monthly download
// counts from the npm downloads API (both keyless).
type npmClient struct {
	hc     *http.Client
	base   string // https://registry.npmjs.org
	dlBase string // https://api.npmjs.org/downloads
}

type npmResponse struct {
	DistTags   map[string]string `json:"dist-tags"` // "latest" -> version
	Time       map[string]string `json:"time"`      // "created", "modified", and per-version timestamps
	Versions   map[string]any    `json:"versions"`
	Repository struct {
		URL string `json:"url"`
	} `json:"repository"`
}

type npmDownloads struct {
	Downloads int64 `json:"downloads"`
}

func (c *npmClient) Meta(ctx context.Context, pkg Package) (RegistryMeta, error) {
	var resp npmResponse
	u := c.base + "/" + npmEscape(pkg.Name)
	if err := getJSON(ctx, c.hc, u, &resp); err != nil {
		return RegistryMeta{}, err
	}

	m := RegistryMeta{Found: true, ReleaseCount: len(resp.Versions), LatestVersion: resp.DistTags["latest"]}

	if created, ok := resp.Time["created"]; ok {
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			m.FirstPublished, m.FirstKnown = t, true
		}
	}
	if modified, ok := resp.Time["modified"]; ok {
		if t, err := time.Parse(time.RFC3339, modified); err == nil {
			m.LatestRelease, m.LatestKnown = t, true
		}
	}

	if u := normalizeRepoURL(resp.Repository.URL); isCodeHost(u) {
		m.RepoURL = u
	}

	var dl npmDownloads
	du := c.dlBase + "/point/last-month/" + npmEscape(pkg.Name)
	if err := getJSON(ctx, c.hc, du, &dl); err == nil {
		m.Downloads, m.DownloadsKnown = dl.Downloads, true
	}

	return m, nil
}

// npmEscape path-escapes a package name. Scoped names ("@scope/name") need the
// slash encoded as %2F, which url.PathEscape does.
func npmEscape(name string) string {
	return url.PathEscape(name)
}
