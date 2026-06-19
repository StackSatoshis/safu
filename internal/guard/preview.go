package guard

import (
	"io/fs"
	"os"
	"path/filepath"
)

// previewCap bounds how many entries the preview walk will count before
// reporting an approximate ("≥") result, so previewing a huge tree stays fast.
const previewCap = 50000

// Preview summarizes what a destructive command would affect (SPEC.md §4.2).
type Preview struct {
	Files    int
	Dirs     int
	Bytes    int64
	Capped   bool     // traversal hit previewCap; counts are lower bounds
	Targets  []string // resolved target paths
	Warnings []string // "looks wrong" signals
}

// BuildPreview walks the command's existing targets to count files/dirs/bytes
// and collect danger signals. It only reads the filesystem (no execution).
func BuildPreview(cmd Command, env Env) *Preview {
	p := &Preview{}
	for _, t := range cmd.Targets {
		p.Targets = append(p.Targets, t.Abs)

		if isHome(t.Abs, env) {
			p.Warnings = append(p.Warnings, "target resolves to your home directory")
		}
		if isFSRoot(t.Abs, env) {
			p.Warnings = append(p.Warnings, "target is a filesystem root")
		}
		if t.Exists && isGitRepoRoot(t.Abs) {
			p.Warnings = append(p.Warnings, "target "+t.Abs+" is a git repository root")
		}

		if !t.Exists {
			continue
		}
		if !t.IsDir {
			p.Files++
			if info, err := os.Lstat(t.Abs); err == nil {
				p.Bytes += info.Size()
			}
			continue
		}
		walkCount(t.Abs, p)
	}
	return p
}

func walkCount(root string, p *Preview) {
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if p.Files+p.Dirs >= previewCap {
			p.Capped = true
			return filepath.SkipAll
		}
		if d.IsDir() {
			p.Dirs++
			return nil
		}
		p.Files++
		if info, err := d.Info(); err == nil {
			p.Bytes += info.Size()
		}
		return nil
	})
}
