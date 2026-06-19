package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/StackSatoshis/safu/internal/config"
)

const (
	markerStart = "# >>> safu bundle >>>"
	markerEnd   = "# <<< safu bundle <<<"
)

// Paths are the filesystem targets for an install.
type Paths struct {
	RC      string // shell rc file
	Config  string // config.toml
	SafuDir string // ~/.safu (where the uninstaller is written)
}

// Result reports what an install created, for reversibility.
type Result struct {
	RCBackup     string
	ConfigBackup string
	Uninstaller  string
}

// Install writes the bundle: it backs up the rc and config (timestamped), saves
// the bundle config, installs the marked rc block (replacing any prior bundle
// block — idempotent), and writes a standalone uninstaller. It assumes the
// caller has already previewed the manifest and obtained confirmation.
func Install(m Manifest, p Paths, now time.Time) (Result, error) {
	var res Result
	ts := now.UTC().Format("20060102-150405")

	// Back up + write config.
	if backup, err := backupIfExists(p.Config, ts); err != nil {
		return res, err
	} else {
		res.ConfigBackup = backup
	}
	if err := config.Write(m.Config, p.Config); err != nil {
		return res, err
	}

	// Back up the rc, then replace/append the marked block.
	existing, err := readFile(p.RC)
	if err != nil {
		return res, err
	}
	if strings.TrimSpace(existing) != "" {
		backup := p.RC + ".safu-bundle-backup-" + ts
		if err := os.WriteFile(backup, []byte(existing), 0o644); err != nil {
			return res, fmt.Errorf("write rc backup: %w", err)
		}
		res.RCBackup = backup
	}

	stripped, _ := stripBlock(existing)
	block := markerStart + "\n" + strings.TrimRight(m.RCBlock, "\n") + "\n" + markerEnd + "\n"
	var out strings.Builder
	out.WriteString(strings.TrimRight(stripped, "\n"))
	if strings.TrimSpace(stripped) != "" {
		out.WriteString("\n\n")
	}
	out.WriteString(block)

	if err := os.MkdirAll(filepath.Dir(p.RC), 0o755); err != nil {
		return res, fmt.Errorf("create rc dir: %w", err)
	}
	if err := os.WriteFile(p.RC, []byte(out.String()), 0o644); err != nil {
		return res, fmt.Errorf("write rc: %w", err)
	}

	// Standalone uninstaller (works without safu, §11.3).
	uninstaller := filepath.Join(p.SafuDir, "bundle-uninstall.sh")
	if err := os.MkdirAll(p.SafuDir, 0o755); err != nil {
		return res, fmt.Errorf("create safu dir: %w", err)
	}
	if err := os.WriteFile(uninstaller, []byte(uninstallScript(p.RC)), 0o755); err != nil {
		return res, fmt.Errorf("write uninstaller: %w", err)
	}
	res.Uninstaller = uninstaller

	return res, nil
}

// Uninstall removes the bundle block from the rc, preserving any later edits.
// Returns whether a block was found.
func Uninstall(rcPath string) (bool, error) {
	content, err := readFile(rcPath)
	if err != nil {
		return false, err
	}
	stripped, found := stripBlock(content)
	if !found {
		return false, nil
	}
	if err := os.WriteFile(rcPath, []byte(strings.TrimRight(stripped, "\n")+"\n"), 0o644); err != nil {
		return false, fmt.Errorf("write rc: %w", err)
	}
	return true, nil
}

// IsInstalled reports whether rc content already contains a bundle block.
func IsInstalled(rcContent string) bool { return strings.Contains(rcContent, markerStart) }

// stripBlock removes the marker-delimited bundle block (inclusive) and reports
// whether one was present.
func stripBlock(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	var out []string
	skip, found := false, false
	for _, line := range lines {
		switch strings.TrimRight(line, " \t") {
		case markerStart:
			skip, found = true, true
			continue
		case markerEnd:
			skip = false
			continue
		}
		if !skip {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n"), found
}

func backupIfExists(path, ts string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	backup := path + ".safu-bundle-backup-" + ts
	if err := os.WriteFile(backup, data, 0o644); err != nil {
		return "", fmt.Errorf("write backup %s: %w", backup, err)
	}
	return backup, nil
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

// uninstallScript is a pure-sh script (awk only, no safu) that strips the
// bundle block from the given rc — so uninstall works even if the binary is
// broken (§11.3).
func uninstallScript(rcPath string) string {
	return `#!/bin/sh
# safu bundle uninstaller. Removes the safu bundle block from your shell rc.
# Pure shell — works even if the safu binary is broken.
RC='` + rcPath + `'
START='` + markerStart + `'
END='` + markerEnd + `'
if [ ! -f "$RC" ]; then
  echo "no rc file at $RC — nothing to do"
  exit 0
fi
TMP="$RC.safu-uninstall.tmp"
awk -v s="$START" -v e="$END" '
  $0==s { skip=1; next }
  $0==e { skip=0; next }
  skip==0 { print }
' "$RC" > "$TMP" && mv "$TMP" "$RC"
echo "removed the safu bundle block from $RC"
echo "a pre-bundle backup may exist alongside it: $RC.safu-bundle-backup-*"
`
}
