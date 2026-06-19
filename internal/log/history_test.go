package log

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryAppendReadSearch(t *testing.T) {
	dir := t.TempDir()
	h := NewHistory(dir, 90)
	for _, c := range []string{"git status", "npm install lodash", "git push"} {
		if err := h.Append(HistoryEntry{Command: c}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := h.Read("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("read %d, want 3", len(all))
	}
	if all[0].Time.IsZero() {
		t.Error("Append should stamp Time")
	}
	git, _ := h.Read("git")
	if len(git) != 2 {
		t.Errorf("search git = %d, want 2", len(git))
	}
}

func TestHistoryDedupLatest(t *testing.T) {
	entries := []HistoryEntry{
		{Command: "ls", Time: time.Unix(1, 0)},
		{Command: "git status", Time: time.Unix(2, 0)},
		{Command: "ls", Time: time.Unix(3, 0)},
	}
	got := DedupLatest(entries)
	if len(got) != 2 {
		t.Fatalf("dedup = %d, want 2", len(got))
	}
	// "git status" then the latest "ls" (order = position of last occurrence).
	if got[0].Command != "git status" || got[1].Command != "ls" {
		t.Errorf("dedup order = %+v", got)
	}
	if got[1].Time != time.Unix(3, 0) {
		t.Errorf("dedup kept the wrong (not latest) ls")
	}
}

func TestHistoryExcluded(t *testing.T) {
	patterns := []string{"*token*", "*secret*", "*password*"}
	cases := []struct {
		cmd  string
		want bool
	}{
		{"export GH_TOKEN=abc123", true}, // case-insensitive contains
		{"echo $MY_SECRET", true},
		{"mysql -p PASSWORD", true},
		{"git status", false},
		{"npm install", false},
	}
	for _, c := range cases {
		if got := HistoryExcluded(c.cmd, patterns); got != c.want {
			t.Errorf("HistoryExcluded(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestHistoryTrimAndClear(t *testing.T) {
	dir := t.TempDir()
	h := NewHistory(dir, 7)
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }
	_ = h.Append(HistoryEntry{Command: "old", Time: now.AddDate(0, 0, -30)})
	_ = h.Append(HistoryEntry{Command: "fresh", Time: now.AddDate(0, 0, -1)})

	if err := h.Trim(); err != nil {
		t.Fatal(err)
	}
	got, _ := h.Read("")
	if len(got) != 1 || got[0].Command != "fresh" {
		t.Errorf("after Trim = %+v, want only fresh", got)
	}

	if err := h.Clear(); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.Read(""); len(got) != 0 {
		t.Errorf("after Clear = %+v, want empty", got)
	}
	// Clearing a missing file is fine.
	if err := NewHistory(filepath.Join(dir, "nope"), 0).Clear(); err != nil {
		t.Errorf("Clear missing: %v", err)
	}
}
