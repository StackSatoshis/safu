package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	markerStart = "# >>> safu shell integration >>>"
	markerEnd   = "# <<< safu shell integration <<<"
)

// Detect guesses the user's shell from $SHELL.
func Detect() (Shell, error) {
	base := filepath.Base(os.Getenv("SHELL"))
	return Parse(base)
}

// DefaultRC returns the conventional rc file path for a shell.
func DefaultRC(sh Shell) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	switch sh {
	case Bash:
		return filepath.Join(home, ".bashrc"), nil
	case Zsh:
		return filepath.Join(home, ".zshrc"), nil
	case Fish:
		return filepath.Join(home, ".config", "fish", "config.fish"), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", sh)
	}
}

// rcBlock wraps a snippet in the safu markers.
func rcBlock(snippet string) string {
	return markerStart + "\n" + strings.TrimRight(snippet, "\n") + "\n" + markerEnd + "\n"
}

// IsInstalled reports whether rc content already contains a safu block.
func IsInstalled(rcContent string) bool {
	return strings.Contains(rcContent, markerStart)
}

// InstallToRC appends the snippet (wrapped in markers) to rcPath. It is
// idempotent: if a safu block is already present it does nothing and returns
// changed=false. When it does modify a non-empty existing file, it first writes
// a copy to backupPath (callers supply a timestamped name).
func InstallToRC(rcPath, snippet, backupPath string) (changed bool, err error) {
	existing, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", rcPath, err)
	}
	if IsInstalled(string(existing)) {
		return false, nil
	}

	if len(existing) > 0 {
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			return false, fmt.Errorf("create backup dir: %w", err)
		}
		if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
			return false, fmt.Errorf("write backup %s: %w", backupPath, err)
		}
	}

	var buf strings.Builder
	buf.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		buf.WriteString("\n")
	}
	buf.WriteString("\n")
	buf.WriteString(rcBlock(snippet))

	if err := os.MkdirAll(filepath.Dir(rcPath), 0o755); err != nil {
		return false, fmt.Errorf("create rc dir: %w", err)
	}
	if err := os.WriteFile(rcPath, []byte(buf.String()), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", rcPath, err)
	}
	return true, nil
}
