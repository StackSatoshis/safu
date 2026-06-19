// Package fix is safu's correction helper engine (SPEC.md §6): given the
// previous command and its STORED output, it proposes corrected commands.
//
// Invariant #4 (CLAUDE.md): this engine NEVER re-executes the previous command.
// It is a pure function over a Command{Script, Stdout, Stderr, ExitCode} value
// and has no OS or shell dependencies (§6.4) — so it can be reused or compiled
// to WASM later. The shell glue captures output as it happens (no rerun); this
// package only reads what it is given.
package fix

import "strings"

// Command is the previous command and its captured result.
type Command struct {
	Script   string
	Stdout   string
	Stderr   string
	ExitCode int
}

// Rule is a pair of pure functions plus metadata (§6.2). Match reports whether
// the rule applies; Fix returns zero or more ALTERNATIVE corrected
// command-lines (a multi-step fix is a single string joined with " && ").
type Rule struct {
	Name          string
	Priority      int
	RequiresRerun bool
	Match         func(Command) bool
	Fix           func(Command) []string
}

// Correction is one proposed corrected command-line.
type Correction struct {
	Rule    string
	Command string
}

// Engine holds rules ordered by descending priority.
type Engine struct{ rules []Rule }

// New returns an Engine with the given rules sorted by priority (desc).
func New(rules []Rule) *Engine {
	r := append([]Rule(nil), rules...)
	// stable insertion sort keeps equal priorities in declaration order
	for i := 1; i < len(r); i++ {
		for j := i; j > 0 && r[j].Priority > r[j-1].Priority; j-- {
			r[j], r[j-1] = r[j-1], r[j]
		}
	}
	return &Engine{rules: r}
}

// DefaultEngine returns the engine with safu's built-in rule set.
func DefaultEngine() *Engine { return New(builtinRules) }

// Correct returns ranked corrections for cmd, highest-priority first,
// de-duplicated. Rules flagged RequiresRerun are skipped unless allowRerun is
// set or the script is on the read-only allowlist (§6.1). It never executes
// anything.
func (e *Engine) Correct(cmd Command, allowRerun bool) []Correction {
	var out []Correction
	seen := map[string]bool{}
	for _, r := range e.rules {
		if r.RequiresRerun && !allowRerun && !isReadOnly(cmd.Script) {
			continue
		}
		if r.Match == nil || !r.Match(cmd) {
			continue
		}
		for _, alt := range r.Fix(cmd) {
			alt = strings.TrimSpace(alt)
			if alt == "" || alt == strings.TrimSpace(cmd.Script) || seen[alt] {
				continue
			}
			seen[alt] = true
			out = append(out, Correction{Rule: r.Name, Command: alt})
		}
	}
	return out
}

// readOnlyCommands may be safely re-run by a RequiresRerun rule.
var readOnlyCommands = map[string]bool{
	"ls": true, "cat": true, "echo": true, "pwd": true, "which": true,
	"type": true, "find": true, "grep": true, "head": true, "tail": true,
	"stat": true, "file": true, "wc": true, "env": true,
}

func isReadOnly(script string) bool {
	w := firstWord(script)
	if w == "git" {
		sub := nthWord(script, 1)
		return sub == "status" || sub == "log" || sub == "diff" || sub == "show"
	}
	return readOnlyCommands[w]
}

// --- small pure string helpers (no shell parsing beyond whitespace) ---

func firstWord(s string) string { return nthWord(s, 0) }

func nthWord(s string, n int) string {
	f := strings.Fields(s)
	if n < 0 || n >= len(f) {
		return ""
	}
	return f[n]
}

func restAfterFirst(s string) string {
	f := strings.Fields(s)
	if len(f) <= 1 {
		return ""
	}
	return strings.Join(f[1:], " ")
}

// withFirstWord replaces the first whitespace-delimited word.
func withFirstWord(s, word string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return word
	}
	f[0] = word
	return strings.Join(f, " ")
}

// insertAfterFirst inserts a flag right after the command word.
func insertAfterFirst(s, flag string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return s
	}
	out := []string{f[0], flag}
	out = append(out, f[1:]...)
	return strings.Join(out, " ")
}

func hasFlag(s string, flags ...string) bool {
	for _, f := range strings.Fields(s) {
		for _, want := range flags {
			if f == want {
				return true
			}
		}
	}
	return false
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
