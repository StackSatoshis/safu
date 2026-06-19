package log

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 90)

	events := []Event{
		{Kind: KindBlock, Command: "rm -rf /", Risk: "block"},
		{Kind: KindSoftDelete, Command: "rm a.txt"},
		{Kind: KindUndo, Command: "rm a.txt"},
	}
	for _, e := range events {
		if err := l.Append(e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := l.Read(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("read %d events, want 3", len(got))
	}
	if got[0].Time.IsZero() {
		t.Error("Append should stamp Time")
	}

	// Grep filter.
	blocks, _ := l.Read(Filter{Grep: "block"})
	if len(blocks) != 1 || blocks[0].Kind != KindBlock {
		t.Errorf("grep block = %+v", blocks)
	}

	// Kind filter.
	undos, _ := l.Read(Filter{Kinds: []string{KindUndo}})
	if len(undos) != 1 {
		t.Errorf("kind filter undo = %d, want 1", len(undos))
	}
}

func TestReadSince(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 0)
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)
	_ = l.Append(Event{Kind: KindBlock, Time: old})
	_ = l.Append(Event{Kind: KindUndo, Time: recent})

	got, err := l.Read(Filter{Since: time.Now().Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != KindUndo {
		t.Errorf("since filter = %+v, want only recent undo", got)
	}
}

func TestTrimRetention(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 7)
	// Pin "now" so the cutoff is deterministic.
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	_ = l.Append(Event{Kind: KindBlock, Time: now.AddDate(0, 0, -30)}) // stale
	_ = l.Append(Event{Kind: KindUndo, Time: now.AddDate(0, 0, -1)})   // fresh

	if err := l.Trim(); err != nil {
		t.Fatal(err)
	}
	got, _ := l.Read(Filter{})
	if len(got) != 1 || got[0].Kind != KindUndo {
		t.Errorf("after Trim got %+v, want only the fresh undo", got)
	}
}

func TestTrimNoStaleSkipsRewrite(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 7)
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	_ = l.Append(Event{Kind: KindUndo, Time: now.AddDate(0, 0, -1)})

	if err := l.Trim(); err != nil {
		t.Fatal(err)
	}
	got, _ := l.Read(Filter{})
	if len(got) != 1 {
		t.Errorf("Trim dropped a fresh event: %+v", got)
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 0)
	_ = l.Append(Event{Kind: KindBlock})
	if err := l.Clear(); err != nil {
		t.Fatal(err)
	}
	got, _ := l.Read(Filter{})
	if len(got) != 0 {
		t.Errorf("after Clear got %d events, want 0", len(got))
	}
	// Clearing a missing log is not an error.
	if err := New(filepath.Join(dir, "nope"), 0).Clear(); err != nil {
		t.Errorf("Clear missing log errored: %v", err)
	}
}
