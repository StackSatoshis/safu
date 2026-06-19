package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func newTestChecker(t *testing.T, srv *httptest.Server, now time.Time) *Checker {
	return &Checker{
		HTTP:      srv.Client(),
		Base:      srv.URL,
		StatePath: filepath.Join(t.TempDir(), "update-check.json"),
		Now:       func() time.Time { return now },
	}
}

func TestFetchAndCache(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path != releasesPath {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"tag_name":"v1.2.0"}`))
	}))
	defer srv.Close()

	now := time.Unix(1_700_000_000, 0)
	c := newTestChecker(t, srv, now)

	latest, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest != "v1.2.0" {
		t.Errorf("latest = %q, want v1.2.0", latest)
	}
	// State persisted.
	st := c.load()
	if st.LatestVersion != "v1.2.0" || st.LastCheck != now {
		t.Errorf("state not cached: %+v", st)
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1", hits)
	}
}

func TestMaybeThrottles(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	defer srv.Close()

	now := time.Unix(1_700_000_000, 0)
	c := newTestChecker(t, srv, now)

	// First call fetches.
	if _, fetched, _ := c.Maybe(context.Background(), 24*time.Hour); !fetched {
		t.Error("first Maybe should fetch")
	}
	// Second call within the interval uses the cache.
	st, fetched, _ := c.Maybe(context.Background(), 24*time.Hour)
	if fetched {
		t.Error("second Maybe should be throttled (no fetch)")
	}
	if st.LatestVersion != "v2.0.0" {
		t.Errorf("cached state lost: %+v", st)
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1 (throttled)", hits)
	}
}

func TestFetchFailsOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestChecker(t, srv, time.Unix(1_700_000_000, 0))
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Error("expected an error on 500")
	}
	// Maybe should return the (empty) cached state and not panic.
	st, fetched, err := c.Maybe(context.Background(), time.Hour)
	if fetched || err == nil {
		t.Errorf("Maybe on failure: fetched=%v err=%v", fetched, err)
	}
	if st.LatestVersion != "" {
		t.Errorf("unexpected cached version: %+v", st)
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.2.0", "v1.1.0", true},
		{"v1.2.0", "1.2.0", false},
		{"v1.2.1", "v1.2.0", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.0.0", "v1.0.0", false},
		{"v0.9.0", "v1.0.0", false},
		{"v1.2.0", "dev", false}, // dev build: no nag
		{"", "v1.0.0", false},
		{"v1.2.0-rc1", "v1.1.0", true}, // suffix ignored
	}
	for _, c := range cases {
		if got := IsNewer(c.latest, c.current); got != c.want {
			t.Errorf("IsNewer(%q,%q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
