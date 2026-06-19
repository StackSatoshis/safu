package fix

import "strings"

// builtinRules is safu's launch correction set. The model is data-driven, so
// growing toward §6.2's ~50–80 rules is just adding entries here. (§14 leaves
// the exact launch set open; this is a solid, well-tested core.)
var builtinRules = []Rule{
	{
		// Prepend sudo when the command failed on a permission error.
		Name:     "sudo",
		Priority: 90,
		Match: func(c Command) bool {
			if c.ExitCode == 0 || firstWord(c.Script) == "sudo" {
				return false
			}
			// "permission denied" on ./script means a missing execute bit, not
			// that sudo is needed — let chmod_x handle it.
			if strings.HasPrefix(strings.TrimSpace(c.Script), "./") {
				return false
			}
			return containsAny(c.Stderr,
				"permission denied", "operation not permitted",
				"are you root", "must be root", "requires superuser",
				"you cannot perform this operation unless you are root",
			)
		},
		Fix: func(c Command) []string { return []string{"sudo " + c.Script} },
	},
	{
		// git: 'comit' is not a git command. Did you mean ... commit
		Name:     "git_did_you_mean",
		Priority: 80,
		Match: func(c Command) bool {
			return firstWord(c.Script) == "git" &&
				(containsFold(c.Stderr, "is not a git command") || containsFold(c.Stderr, "did you mean"))
		},
		Fix: func(c Command) []string {
			f := strings.Fields(c.Script)
			if len(f) < 2 {
				return nil
			}
			var out []string
			for _, sug := range gitSuggestions(c.Stderr) {
				nf := append([]string(nil), f...)
				nf[1] = sug
				out = append(out, strings.Join(nf, " "))
			}
			return out
		},
	},
	{
		// fatal: The current branch X has no upstream branch.
		Name:     "git_set_upstream",
		Priority: 80,
		Match: func(c Command) bool {
			return firstWord(c.Script) == "git" && containsFold(c.Stderr, "has no upstream branch")
		},
		Fix: func(c Command) []string {
			// git prints the exact command to use; prefer it verbatim.
			for _, line := range strings.Split(c.Stderr, "\n") {
				t := strings.TrimSpace(line)
				if strings.HasPrefix(t, "git push --set-upstream") {
					return []string{t}
				}
			}
			return nil
		},
	},
	{
		// cd into a path whose parents don't exist.
		Name:     "cd_mkdir",
		Priority: 70,
		Match: func(c Command) bool {
			return firstWord(c.Script) == "cd" && containsFold(c.Stderr, "no such file or directory")
		},
		Fix: func(c Command) []string {
			dir := nthWord(c.Script, 1)
			if dir == "" {
				return nil
			}
			return []string{"mkdir -p " + dir + " && cd " + dir}
		},
	},
	{
		// mkdir foo/bar/baz when foo/bar doesn't exist.
		Name:     "mkdir_p",
		Priority: 70,
		Match: func(c Command) bool {
			return firstWord(c.Script) == "mkdir" &&
				containsFold(c.Stderr, "no such file or directory") &&
				!hasFlag(c.Script, "-p", "--parents")
		},
		Fix: func(c Command) []string { return []string{insertAfterFirst(c.Script, "-p")} },
	},
	{
		// cp of a directory without -r.
		Name:     "cp_dir",
		Priority: 70,
		Match: func(c Command) bool {
			// GNU cp: "omitting directory"; BSD/macOS cp: "is a directory".
			return firstWord(c.Script) == "cp" &&
				(containsFold(c.Stderr, "omitting directory") || containsFold(c.Stderr, "is a directory")) &&
				!hasFlag(c.Script, "-r", "-R", "--recursive", "-a")
		},
		Fix: func(c Command) []string { return []string{insertAfterFirst(c.Script, "-r")} },
	},
	{
		// rm of a directory without -r (the guard still protects the rerun).
		Name:     "rm_dir",
		Priority: 60,
		Match: func(c Command) bool {
			return firstWord(c.Script) == "rm" && containsFold(c.Stderr, "is a directory") &&
				!hasFlag(c.Script, "-r", "-R", "--recursive")
		},
		Fix: func(c Command) []string { return []string{insertAfterFirst(c.Script, "-r")} },
	},
	{
		// Running ./script without the execute bit.
		Name:     "chmod_x",
		Priority: 70,
		Match: func(c Command) bool {
			return strings.HasPrefix(strings.TrimSpace(c.Script), "./") &&
				containsFold(c.Stderr, "permission denied")
		},
		Fix: func(c Command) []string {
			f := firstWord(c.Script)
			return []string{"chmod +x " + f + " && " + c.Script}
		},
	},
	{
		// man with no entry -> try the tool's --help.
		Name:     "man_no_entry",
		Priority: 50,
		Match: func(c Command) bool {
			return firstWord(c.Script) == "man" && containsFold(c.Stderr, "no manual entry")
		},
		Fix: func(c Command) []string {
			arg := nthWord(c.Script, 1)
			if arg == "" {
				return nil
			}
			return []string{arg + " --help"}
		},
	},
	{
		// Mistyped or aliased command name on "command not found".
		Name:     "command_typo",
		Priority: 60,
		Match: func(c Command) bool {
			if !containsFold(c.Stderr, "command not found") {
				return false
			}
			_, ok := commandFixes[firstWord(c.Script)]
			return ok
		},
		Fix: func(c Command) []string {
			return []string{withFirstWord(c.Script, commandFixes[firstWord(c.Script)])}
		},
	},
}

// commandFixes maps common mistyped/aliased command names to the intended one.
var commandFixes = map[string]string{
	"gti":    "git",
	"got":    "git",
	"sl":     "ls",
	"grpe":   "grep",
	"gerp":   "grep",
	"mkdri":  "mkdir",
	"suod":   "sudo",
	"claer":  "clear",
	"cler":   "clear",
	"ehco":   "echo",
	"pyhton": "python3",
	"pytohn": "python3",
	"python": "python3",
	"pip":    "pip3",
}

// gitSuggestions extracts the indented command suggestions git prints after
// "The most similar command(s)" / "Did you mean".
func gitSuggestions(stderr string) []string {
	var out []string
	seen := map[string]bool{}
	collecting := false
	for _, line := range strings.Split(stderr, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "most similar") || strings.Contains(lower, "did you mean") {
			collecting = true
			continue
		}
		if !collecting {
			continue
		}
		// suggestions are indented (tab or spaces); stop at a blank line.
		if strings.TrimSpace(line) == "" {
			break
		}
		if line[0] != '\t' && line[0] != ' ' {
			break
		}
		word := strings.Fields(line)
		if len(word) > 0 && !seen[word[0]] {
			seen[word[0]] = true
			out = append(out, word[0])
		}
	}
	return out
}

func containsAny(haystack string, needles ...string) bool {
	low := strings.ToLower(haystack)
	for _, n := range needles {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}
