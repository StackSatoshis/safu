package guard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mkTarget(t *testing.T, path, content string) Target {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Lstat(path)
	return Target{Arg: path, Abs: path, Real: path, Exists: true, IsDir: fi.IsDir()}
}

func TestTrashAndUndo(t *testing.T) {
	base := t.TempDir()
	trashDir := filepath.Join(base, "trash")
	f1 := filepath.Join(base, "work", "a.txt")
	f2 := filepath.Join(base, "work", "b.txt")
	targets := []Target{mkTarget(t, f1, "aaa"), mkTarget(t, f2, "bbb")}

	now := time.Unix(1_700_000_000, 0)
	m, err := Trash(targets, trashDir, "rm -rf a.txt b.txt", now)
	if err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if len(m.Entries) != 2 {
		t.Fatalf("manifest entries = %d, want 2", len(m.Entries))
	}
	// Originals are gone, trash op dir + manifest exist.
	if _, err := os.Lstat(f1); !os.IsNotExist(err) {
		t.Errorf("original %s should be moved away", f1)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "manifest.json")); err != nil {
		t.Errorf("manifest not written: %v", err)
	}

	// Undo restores both.
	if _, err := Undo(trashDir); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	for _, f := range []string{f1, f2} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s not restored: %v", f, err)
		}
	}
	// Op dir removed after full restore.
	if _, err := os.Stat(m.Dir); !os.IsNotExist(err) {
		t.Errorf("trash op dir should be gone after undo")
	}
}

func TestUndoRefusesClobber(t *testing.T) {
	base := t.TempDir()
	trashDir := filepath.Join(base, "trash")
	f := filepath.Join(base, "work", "a.txt")
	targets := []Target{mkTarget(t, f, "aaa")}

	if _, err := Trash(targets, trashDir, "rm a.txt", time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	// Recreate the original path so undo would clobber it.
	mkTarget(t, f, "new content")

	if _, err := Undo(trashDir); err == nil {
		t.Error("Undo should refuse to overwrite an existing path")
	}
}

func TestSweep(t *testing.T) {
	base := t.TempDir()
	trashDir := filepath.Join(base, "trash")

	old := time.Now().AddDate(0, 0, -30)
	recent := time.Now().AddDate(0, 0, -1)
	mkTarget(t, filepath.Join(base, "o.txt"), "x")
	mkTarget(t, filepath.Join(base, "r.txt"), "y")
	if _, err := Trash([]Target{{Arg: "o", Abs: filepath.Join(base, "o.txt"), Real: filepath.Join(base, "o.txt"), Exists: true}}, trashDir, "rm o", old); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash([]Target{{Arg: "r", Abs: filepath.Join(base, "r.txt"), Real: filepath.Join(base, "r.txt"), Exists: true}}, trashDir, "rm r", recent); err != nil {
		t.Fatal(err)
	}

	removed, err := Sweep(trashDir, 7, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("Sweep removed %d, want 1", removed)
	}
	ops, _ := ListOps(trashDir)
	if len(ops) != 1 {
		t.Errorf("after sweep %d ops remain, want 1", len(ops))
	}
}
