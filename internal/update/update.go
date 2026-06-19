// Package update implements safu's opt-out update check — one of the only TWO
// kinds of outbound call safu ever makes (the other is package audits; see
// SPEC.md §10 and CLAUDE.md invariant #1). It queries the GitHub Releases API
// for the latest tag and caches the result locally.
//
// It NEVER runs in the guard/command hot path, sends no user-identifying data
// (just an unauthenticated GET to a public endpoint), and is disabled by
// network.update_check=false, network.offline=true, SAFU_NO_UPDATE_CHECK=1, or
// --no-update-check.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	releasesPath = "/repos/StackSatoshis/safu/releases/latest"
	userAgent    = "safu-update-check (+https://safu.sh)"
	// ReleasesURL is shown to the user when an update is available.
	ReleasesURL = "https://github.com/StackSatoshis/safu/releases/latest"
)

// State is the locally-cached result of the last check.
type State struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version"`
}

// Checker performs and caches update checks.
type Checker struct {
	HTTP      *http.Client
	Base      string // API root, e.g. https://api.github.com
	StatePath string // cache file
	Now       func() time.Time
}

// New returns a Checker caching under dir (typically ~/.safu).
func New(dir string) *Checker {
	return &Checker{
		HTTP:      &http.Client{Timeout: 5 * time.Second},
		Base:      "https://api.github.com",
		StatePath: filepath.Join(dir, "update-check.json"),
		Now:       time.Now,
	}
}

type ghRelease struct {
	TagName string `json:"tag_name"`
}

// Fetch performs the network check, updates the cache, and returns the latest
// release tag.
func (c *Checker) Fetch(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+releasesPath, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("update check: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update check: status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("update check: decode: %w", err)
	}
	c.save(State{LastCheck: c.Now(), LatestVersion: rel.TagName})
	return rel.TagName, nil
}

// Maybe returns the cached state without a network call if the last check was
// within minInterval; otherwise it fetches. The returned bool reports whether a
// network fetch happened. Errors are returned but the cached state is always
// usable (fail open).
func (c *Checker) Maybe(ctx context.Context, minInterval time.Duration) (State, bool, error) {
	st := c.load()
	if !st.LastCheck.IsZero() && c.Now().Sub(st.LastCheck) < minInterval {
		return st, false, nil
	}
	latest, err := c.Fetch(ctx)
	if err != nil {
		return st, false, err // keep the (possibly empty) cached state
	}
	return State{LastCheck: c.Now(), LatestVersion: latest}, true, nil
}

func (c *Checker) load() State {
	var st State
	data, err := os.ReadFile(c.StatePath)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	return st
}

func (c *Checker) save(st State) {
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.StatePath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(c.StatePath, data, 0o644)
}

// IsNewer reports whether latest is a strictly higher semantic version than
// current. Non-semver or "dev" current versions return false (no nag).
func IsNewer(latest, current string) bool {
	lv, lok := parseVer(latest)
	cv, cok := parseVer(current)
	if !lok || !cok {
		return false
	}
	for i := 0; i < 3; i++ {
		if lv[i] != cv[i] {
			return lv[i] > cv[i]
		}
	}
	return false
}

// parseVer parses "v1.2.3" / "1.2.3-rc1" into [3]int, ignoring any pre-release
// or build suffix. ok=false for anything non-numeric (e.g. "dev").
func parseVer(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return out, false
	}
	parts := strings.Split(s, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
