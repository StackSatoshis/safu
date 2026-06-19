// Package guard parses a destructive shell command, classifies its risk
// against the active protection level, previews its effect, and decides whether
// to proceed (SPEC.md §4). It performs NO network I/O (invariant #1) and never
// executes the command to analyze it — globs are already expanded by the shell
// and paths are resolved statically.
package guard

import (
	"os"
	"path/filepath"
	"time"
)

// Risk is the verdict for a guarded invocation.
type Risk string

const (
	Safe  Risk = "safe"
	Warn  Risk = "warn"
	Block Risk = "block"
)

func riskRank(r Risk) int {
	switch r {
	case Block:
		return 2
	case Warn:
		return 1
	default:
		return 0
	}
}

// max returns the higher-risk of two verdicts.
func maxRisk(a, b Risk) Risk {
	if riskRank(b) > riskRank(a) {
		return b
	}
	return a
}

// Protection levels (SPEC.md §4.1), ranked.
func levelRank(level string) int {
	switch level {
	case "light":
		return 1
	case "standard":
		return 2
	case "paranoid":
		return 3
	default: // "off" or unknown
		return 0
	}
}

// Env is the resolved environment a classification runs against. Injecting it
// keeps the rules pure and unit-testable.
type Env struct {
	Home       string
	Cwd        string
	MountRoots []string // absolute paths treated as filesystem/mount roots
	Now        func() time.Time
}

// CurrentEnv builds an Env from the live process environment.
func CurrentEnv() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return Env{}, err
	}
	return Env{
		Home:       filepath.Clean(home),
		Cwd:        filepath.Clean(cwd),
		MountRoots: detectMountRoots(),
		Now:        time.Now,
	}, nil
}

// detectMountRoots returns paths that should be treated as a root of a
// filesystem (deleting into them is catastrophic). "/" is always included;
// common mount parents are added best-effort.
func detectMountRoots() []string {
	roots := []string{"/"}
	for _, p := range []string{"/Volumes", "/mnt", "/media", "/home", "/Users"} {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			roots = append(roots, p)
		}
	}
	return roots
}

// Target is a resolved positional path argument.
type Target struct {
	Arg    string // original argument
	Abs    string // cleaned absolute path
	Real   string // EvalSymlinks result (best-effort; falls back to Abs)
	Exists bool
	IsDir  bool
}

// Command is the parsed form of a guarded invocation.
type Command struct {
	Name      string            // rm, dd, mkfs, chmod, chown, git, ...
	Args      []string          // raw argv (after the "--")
	Recursive bool              // -r / -R / --recursive
	Force     bool              // -f / --force present in the wrapped command itself
	Options   map[string]string // command-specific: dd "of", chmod "mode", git "subcommand"/"push_force"
	Targets   []Target          // resolved positional path targets
}

// Finding is one contributing risk signal.
type Finding struct {
	Rule       string
	Risk       Risk
	Reason     string
	Suggestion string
}

// Decision is the classified outcome, optionally with a filesystem preview.
type Decision struct {
	Command  Command
	Risk     Risk
	Findings []Finding
	Preview  *Preview
}

// IsDelete reports whether the command removes files (eligible for soft-delete).
func (c Command) IsDelete() bool { return c.Name == "rm" }
