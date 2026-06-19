package audit

import (
	"context"
	"net/http"
	"net/url"
)

// homebrewClient reads formula metadata from the formulae.brew.sh API.
//
// Homebrew is not covered by OSV (no malicious/CVE feed), and the formula API
// does not expose a reliable first-publish date, so this client contributes
// mainly the linked repo (homepage) and install analytics where present. These
// gaps are intentional and surfaced as "unknown", never as false warnings.
type homebrewClient struct {
	hc   *http.Client
	base string // https://formulae.brew.sh/api
}

type homebrewResponse struct {
	Homepage string `json:"homepage"`
	Versions struct {
		Stable string `json:"stable"`
	} `json:"versions"`
	Analytics struct {
		Install map[string]map[string]int64 `json:"install"` // e.g. {"30d": {"<name>": 12345}}
	} `json:"analytics"`
}

func (c *homebrewClient) Meta(ctx context.Context, pkg Package) (RegistryMeta, error) {
	var resp homebrewResponse
	u := c.base + "/formula/" + url.PathEscape(pkg.Name) + ".json"
	if err := getJSON(ctx, c.hc, u, &resp); err != nil {
		return RegistryMeta{}, err
	}

	m := RegistryMeta{Found: true, LatestVersion: resp.Versions.Stable}
	if isCodeHost(resp.Homepage) {
		m.RepoURL = normalizeRepoURL(resp.Homepage)
	}

	// 30-day install count, if the analytics block is present.
	if d30, ok := resp.Analytics.Install["30d"]; ok {
		if n, ok := d30[pkg.Name]; ok {
			m.Downloads, m.DownloadsKnown = n, true
		}
	}

	return m, nil
}
