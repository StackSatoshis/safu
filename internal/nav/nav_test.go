package nav

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixedClock(ts int64) func() time.Time {
	return func() time.Time { return time.Unix(ts, 0) }
}

func TestFrecencyBuckets(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	cases := []struct {
		ageSec int64
		want   float64
	}{
		{60, 40},         // < 1h  -> ×4
		{7200, 20},       // < 1d  -> ×2
		{200000, 5},      // < 1w  -> ×0.5
		{2_000_000, 2.5}, // older -> ×0.25
	}
	for _, c := range cases {
		e := Entry{Rank: 10, LastAccess: now.Unix() - c.ageSec}
		if got := frecency(e, now); got != c.want {
			t.Errorf("age %ds: frecency = %v, want %v", c.ageSec, got, c.want)
		}
	}
}

func TestAddPersistsAndAccumulates(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "proj")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	db, err := Open(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	db.now = fixedClock(1_000_000)
	for i := 0; i < 3; i++ {
		if err := db.Add(target); err != nil {
			t.Fatal(err)
		}
	}

	// Reopen: the data should persist.
	db2, err := Open(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := db2.entries[target]
	if !ok {
		t.Fatalf("entry not persisted")
	}
	if e.Rank != 3 {
		t.Errorf("rank = %v, want 3", e.Rank)
	}
}

func TestAddIgnoresRelative(t *testing.T) {
	db, _ := Open(t.TempDir(), 100)
	if err := db.Add("relative/path"); err != nil {
		t.Fatal(err)
	}
	if len(db.entries) != 0 {
		t.Errorf("relative path should be ignored")
	}
}

func TestQueryPicksHighestFrecency(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	work := mk("workspace")
	worktree := mk("worktree")

	db, _ := Open(dir, 100)
	db.now = fixedClock(1_000_000)
	db.entries[work] = &Entry{Path: work, Rank: 1, LastAccess: 1_000_000}
	db.entries[worktree] = &Entry{Path: worktree, Rank: 10, LastAccess: 1_000_000}

	got, ok := db.Query("work")
	if !ok {
		t.Fatal("expected a match")
	}
	if got != worktree {
		t.Errorf("Query = %q, want %q (higher frecency)", got, worktree)
	}
}

func TestQueryLiteralDir(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir, 100)
	got, ok := db.Query(dir)
	if !ok || got != dir {
		t.Errorf("literal dir query = (%q,%v), want (%q,true)", got, ok, dir)
	}
}

func TestQueryPrunesDeadPaths(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone")
	db, _ := Open(dir, 100)
	db.entries[gone] = &Entry{Path: gone, Rank: 5, LastAccess: 1_000_000}

	if _, ok := db.Query("gone"); ok {
		t.Error("dead path should not match")
	}
	if _, ok := db.entries[gone]; ok {
		t.Error("dead path should be pruned")
	}
}

func TestQuerySubsequenceAndBasename(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "src", "safu", "internal")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	db, _ := Open(dir, 100)
	db.now = fixedClock(1_000_000)
	db.entries[deep] = &Entry{Path: deep, Rank: 5, LastAccess: 1_000_000}

	// "safu internal" matches in order, last token in basename.
	if _, ok := db.Query("safu internal"); !ok {
		t.Error("expected ordered subsequence match")
	}
	// last token must be in the basename: "internal safu" should NOT match
	// (basename is "internal", not "safu").
	if _, ok := db.Query("internal safu"); ok {
		t.Error("did not expect match when last token is not in basename")
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	_ = os.MkdirAll(p, 0o755)
	db, _ := Open(dir, 100)
	_ = db.Add(p)
	if err := db.Clear(); err != nil {
		t.Fatal(err)
	}
	if len(db.List()) != 0 {
		t.Error("List not empty after Clear")
	}
	db2, _ := Open(dir, 100)
	if len(db2.entries) != 0 {
		t.Error("cleared db should load empty")
	}
}

func TestEviction(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir, 2)
	db.now = fixedClock(1_000_000)
	for i, name := range []string{"a", "b", "c"} {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(p, 0o755)
		db.entries[p] = &Entry{Path: p, Rank: float64(i + 1), LastAccess: 1_000_000}
	}
	db.evict()
	if len(db.entries) != 2 {
		t.Errorf("after evict %d entries, want 2", len(db.entries))
	}
	// Lowest-ranked ("a") should be gone.
	if _, ok := db.entries[filepath.Join(dir, "a")]; ok {
		t.Error("lowest-frecency entry should be evicted")
	}
}

func TestIsExcluded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := []struct {
		path     string
		patterns []string
		want     bool
	}{
		{home, []string{"~"}, true},
		{filepath.Join(home, "private", "x"), []string{"$HOME/private/*"}, true},
		{filepath.Join(home, "work"), []string{"$HOME/private/*"}, false},
		{"/tmp/anything", []string{"~", "$HOME/private/*"}, false},
	}
	for _, c := range cases {
		if got := IsExcluded(c.path, c.patterns); got != c.want {
			t.Errorf("IsExcluded(%q, %v) = %v, want %v", c.path, c.patterns, got, c.want)
		}
	}
}
