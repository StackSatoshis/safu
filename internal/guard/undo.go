package guard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ListOps returns the soft-delete operations in trashDir, newest first.
func ListOps(trashDir string) ([]Manifest, error) {
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read trash dir: %w", err)
	}
	var ops []Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := readManifest(filepath.Join(trashDir, e.Name()))
		if err != nil {
			continue // ignore dirs without a valid manifest
		}
		ops = append(ops, m)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].OpID > ops[j].OpID })
	return ops, nil
}

func readManifest(opDir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(opDir, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, err
	}
	m.Dir = opDir
	return m, nil
}

// Undo restores the most recent soft-delete operation, moving each entry back
// to its original path. It refuses to clobber a path that now exists, and
// removes the op directory only if every entry was restored.
func Undo(trashDir string) (Manifest, error) {
	ops, err := ListOps(trashDir)
	if err != nil {
		return Manifest{}, err
	}
	if len(ops) == 0 {
		return Manifest{}, fmt.Errorf("nothing to undo")
	}
	m := ops[0]

	var restored int
	for _, e := range m.Entries {
		if _, err := os.Lstat(e.Original); err == nil {
			return m, fmt.Errorf("refusing to overwrite %s (restore the rest manually)", e.Original)
		}
		stored := filepath.Join(m.Dir, "files", e.Stored)
		if err := os.MkdirAll(filepath.Dir(e.Original), 0o755); err != nil {
			return m, fmt.Errorf("recreate parent of %s: %w", e.Original, err)
		}
		if err := move(stored, e.Original); err != nil {
			return m, fmt.Errorf("restore %s: %w", e.Original, err)
		}
		restored++
	}

	if restored == len(m.Entries) {
		if err := os.RemoveAll(m.Dir); err != nil {
			return m, fmt.Errorf("clean up trash op: %w", err)
		}
	}
	return m, nil
}

// Sweep removes trash operations older than retentionDays (the on-invocation
// purge, §4.3 — no daemon). retentionDays <= 0 disables it.
func Sweep(trashDir string, retentionDays int, now time.Time) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	ops, err := ListOps(trashDir)
	if err != nil {
		return 0, err
	}
	cutoff := now.AddDate(0, 0, -retentionDays)
	removed := 0
	for _, m := range ops {
		if m.Time.Before(cutoff) {
			if err := os.RemoveAll(m.Dir); err != nil {
				return removed, fmt.Errorf("purge %s: %w", m.Dir, err)
			}
			removed++
		}
	}
	return removed, nil
}
