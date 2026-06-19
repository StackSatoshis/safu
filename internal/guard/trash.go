package guard

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// TrashEntry records one moved path so it can be restored (SPEC.md §4.3).
type TrashEntry struct {
	Original string      `json:"original"`
	Stored   string      `json:"stored"` // path relative to the op dir's files/
	Mode     os.FileMode `json:"mode"`
	ModTime  time.Time   `json:"mod_time"`
	IsDir    bool        `json:"is_dir"`
}

// Manifest is the sidecar describing one soft-delete operation.
type Manifest struct {
	OpID    string       `json:"op_id"`
	Time    time.Time    `json:"time"`
	Command string       `json:"command"`
	Dir     string       `json:"-"` // absolute op dir (not serialized)
	Entries []TrashEntry `json:"entries"`
}

// Trash moves the command's existing targets into trashDir/<opID>/files/,
// preserving metadata in a manifest. opID is derived from now (caller passes a
// clock for determinism). It returns the written manifest.
func Trash(targets []Target, trashDir, command string, now time.Time) (Manifest, error) {
	opID := now.UTC().Format("20060102-150405.000000000")
	opDir := filepath.Join(trashDir, opID)
	filesDir := filepath.Join(opDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("create trash dir: %w", err)
	}

	m := Manifest{OpID: opID, Time: now.UTC(), Command: command, Dir: opDir}
	seen := map[string]int{}
	for _, t := range targets {
		if !t.Exists {
			continue
		}
		fi, err := os.Lstat(t.Abs)
		if err != nil {
			return m, fmt.Errorf("stat %s: %w", t.Abs, err)
		}
		// Avoid collisions when two targets share a basename.
		base := filepath.Base(t.Abs)
		if n := seen[base]; n > 0 {
			base = fmt.Sprintf("%s.%d", base, n)
		}
		seen[filepath.Base(t.Abs)]++

		stored := filepath.Join(filesDir, base)
		if err := move(t.Abs, stored); err != nil {
			return m, fmt.Errorf("move %s to trash: %w", t.Abs, err)
		}
		m.Entries = append(m.Entries, TrashEntry{
			Original: t.Abs,
			Stored:   base,
			Mode:     fi.Mode(),
			ModTime:  fi.ModTime(),
			IsDir:    fi.IsDir(),
		})
	}

	if err := writeManifest(opDir, m); err != nil {
		return m, err
	}
	return m, nil
}

func writeManifest(opDir string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(opDir, "manifest.json"), data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

// move renames src to dst, falling back to a recursive copy+remove when the two
// live on different filesystems (os.Rename fails with EXDEV).
func move(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func copyTree(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case fi.IsDir():
		if err := os.MkdirAll(dst, fi.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	default:
		return copyFile(src, dst, fi.Mode())
	}
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
