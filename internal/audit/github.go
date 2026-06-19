package audit

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// RepoSignals are the source-repository health signals safu reads from GitHub.
type RepoSignals struct {
	Stars      int
	LastCommit time.Time
	Archived   bool
	OpenIssues int
}

// githubClient fetches repo signals from the GitHub REST API (unauthenticated;
// 60 req/hr/IP). Only github.com repos are supported; other hosts yield no
// signals (but the repo link still counts, so the package is not "no-repo").
type githubClient struct {
	hc   *http.Client
	base string // https://api.github.com
}

type githubRepoResponse struct {
	StargazersCount int       `json:"stargazers_count"`
	PushedAt        time.Time `json:"pushed_at"`
	Archived        bool      `json:"archived"`
	OpenIssuesCount int       `json:"open_issues_count"`
}

func (c *githubClient) Signals(ctx context.Context, repoURL string) (RepoSignals, error) {
	owner, repo, ok := parseGitHubRepo(repoURL)
	if !ok {
		// Not a GitHub repo — no signals available, but not an error worth
		// failing open over.
		return RepoSignals{}, nil
	}
	var resp githubRepoResponse
	u := c.base + "/repos/" + owner + "/" + repo
	if err := getJSON(ctx, c.hc, u, &resp); err != nil {
		return RepoSignals{}, err
	}
	return RepoSignals{
		Stars:      resp.StargazersCount,
		LastCommit: resp.PushedAt,
		Archived:   resp.Archived,
		OpenIssues: resp.OpenIssuesCount,
	}, nil
}

// normalizeRepoURL strips common decorations from a registry-provided repo URL
// (git+ prefixes, .git suffixes, ssh forms) into a plain https URL.
func normalizeRepoURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	u = strings.TrimPrefix(u, "git+")
	u = strings.TrimSuffix(u, ".git")
	// git@github.com:owner/repo -> https://github.com/owner/repo
	if strings.HasPrefix(u, "git@") {
		u = strings.Replace(u, ":", "/", 1)
		u = "https://" + strings.TrimPrefix(u, "git@")
	}
	u = strings.TrimPrefix(u, "ssh://")
	u = strings.TrimPrefix(u, "git://")
	if strings.HasPrefix(u, "http://") {
		u = "https://" + strings.TrimPrefix(u, "http://")
	}
	if !strings.HasPrefix(u, "https://") && isCodeHost(u) {
		u = "https://" + u
	}
	return u
}

// parseGitHubRepo extracts owner and repo from a github.com URL.
func parseGitHubRepo(repoURL string) (owner, repo string, ok bool) {
	u := normalizeRepoURL(repoURL)
	const marker = "github.com/"
	i := strings.Index(strings.ToLower(u), marker)
	if i < 0 {
		return "", "", false
	}
	rest := u[i+len(marker):]
	rest = strings.TrimSuffix(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
