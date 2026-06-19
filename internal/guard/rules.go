package guard

import (
	"os"
	"path/filepath"
	"strings"
)

// Classify runs the rule set for the active protection level and returns the
// combined Decision. Level "off" (or unknown) is a no-op pass-through (Safe).
func Classify(cmd Command, level string, env Env) Decision {
	d := Decision{Command: cmd, Risk: Safe}
	if levelRank(level) == 0 {
		return d
	}

	switch cmd.Name {
	case "rm":
		d.Findings = classifyRm(cmd, level, env)
	case "dd":
		d.Findings = classifyDevice(cmd, level, env, "dd-device", "dd is writing directly to")
	case "mkfs":
		d.Findings = classifyDevice(cmd, level, env, "mkfs-device", "mkfs will format")
	case "chmod":
		d.Findings = classifyChmod(cmd, level, env)
	case "chown":
		d.Findings = classifyChown(cmd, level, env)
	case "git":
		d.Findings = classifyGit(cmd, level)
	}

	for _, f := range d.Findings {
		d.Risk = maxRisk(d.Risk, f.Risk)
	}
	if riskRank(d.Risk) >= riskRank(Warn) && cmd.IsDelete() {
		d.Preview = BuildPreview(cmd, env)
	}
	return d
}

func classifyRm(cmd Command, level string, env Env) []Finding {
	var fs []Finding
	for _, t := range cmd.Targets {
		// rm removes the symlink itself, not its target, so danger identity is
		// judged on the cleaned absolute path (not the symlink resolution).
		p := t.Abs

		if cmd.Recursive {
			switch {
			case isFSRoot(p, env):
				fs = append(fs, Finding{"rm-root", Block,
					"refusing to recursively delete a filesystem root (" + p + ")",
					"target a specific subdirectory instead"})
				continue
			case isHome(p, env):
				fs = append(fs, Finding{"rm-home", Block,
					"refusing to recursively delete your home directory",
					"target a specific subdirectory of $HOME"})
				continue
			case aboveCwd(p, env):
				fs = append(fs, Finding{"rm-above-cwd", Block,
					"target is a parent of the current directory (" + p + ")",
					"cd elsewhere, or remove a child path"})
				continue
			}
			if outsideCwd(p, env) && levelRank(level) >= 2 {
				fs = append(fs, Finding{"rm-outside-cwd", Warn,
					"recursively deleting a path outside the current directory (" + p + ")", ""})
			}
			if levelRank(level) >= 2 && t.Exists && isGitRepoRoot(p) {
				fs = append(fs, Finding{"rm-git-root", Warn,
					"target is a git repository root (" + p + ")", ""})
			}
		} else if levelRank(level) >= 3 && outsideCwd(p, env) {
			// paranoid: confirm anything outside CWD, even non-recursive.
			fs = append(fs, Finding{"rm-outside-cwd-paranoid", Warn,
				"removing a path outside the current directory (" + p + ")", ""})
		}
	}
	return fs
}

// classifyDevice handles dd (of=) and mkfs: writing to a block device or any
// /dev/* path is catastrophic and blocks unless the user forces it.
func classifyDevice(cmd Command, level string, env Env, rule, verb string) []Finding {
	var fs []Finding
	for _, t := range cmd.Targets {
		p := t.Real
		if isDevicePath(p) {
			fs = append(fs, Finding{rule, Block,
				verb + " device " + p,
				"double-check the target is a file, not a disk; re-run with --force to override"})
		}
	}
	return fs
}

func classifyChmod(cmd Command, level string, env Env) []Finding {
	var fs []Finding
	mode := cmd.Options["mode"]
	for _, t := range cmd.Targets {
		p := t.Abs
		if levelRank(level) >= 2 && isSystemPath(p) && outsideCwd(p, env) {
			fs = append(fs, Finding{"chmod-system", Warn,
				"changing permissions on a system path (" + p + ")", ""})
		}
		if cmd.Recursive && worldWritable(mode) && outsideCwd(p, env) && levelRank(level) >= 2 {
			fs = append(fs, Finding{"chmod-r-world", Warn,
				"recursive world-writable chmod outside the current directory (" + p + ")",
				"scope the chmod to specific files"})
		}
	}
	return fs
}

func classifyChown(cmd Command, level string, env Env) []Finding {
	var fs []Finding
	for _, t := range cmd.Targets {
		p := t.Abs
		if levelRank(level) >= 2 && cmd.Recursive && isSystemPath(p) && outsideCwd(p, env) {
			fs = append(fs, Finding{"chown-system", Warn,
				"recursive chown on a system path (" + p + ")", ""})
		} else if levelRank(level) >= 3 && cmd.Recursive && outsideCwd(p, env) {
			fs = append(fs, Finding{"chown-outside-cwd-paranoid", Warn,
				"recursive chown outside the current directory (" + p + ")", ""})
		}
	}
	return fs
}

func classifyGit(cmd Command, level string) []Finding {
	if levelRank(level) < 2 {
		return nil
	}
	if cmd.Options["subcommand"] == "push" && cmd.Options["push_force"] == "true" &&
		cmd.Options["push_force_with_lease"] != "true" {
		return []Finding{{"git-force-push", Warn,
			"force-pushing can irreversibly overwrite remote history",
			"prefer --force-with-lease"}}
	}
	return nil
}

// --- path predicates ---

func isFSRoot(p string, env Env) bool {
	p = filepath.Clean(p)
	if p == "/" {
		return true
	}
	for _, r := range env.MountRoots {
		if filepath.Clean(r) == p {
			return true
		}
	}
	return false
}

func isHome(p string, env Env) bool {
	return env.Home != "" && filepath.Clean(p) == filepath.Clean(env.Home)
}

func within(base, p string) bool {
	base, p = filepath.Clean(base), filepath.Clean(p)
	if p == base {
		return true
	}
	return strings.HasPrefix(p, base+string(filepath.Separator))
}

// aboveCwd reports whether p is a strict ancestor of the current directory.
func aboveCwd(p string, env Env) bool {
	anc, cwd := filepath.Clean(p), filepath.Clean(env.Cwd)
	return anc != cwd && strings.HasPrefix(cwd, anc+string(filepath.Separator))
}

func outsideCwd(p string, env Env) bool { return !within(env.Cwd, p) }

func isGitRepoRoot(p string) bool {
	_, err := os.Stat(filepath.Join(p, ".git"))
	return err == nil
}

var systemPrefixes = []string{"/usr", "/bin", "/sbin", "/etc", "/var", "/lib", "/sys", "/boot", "/proc", "/opt"}

func isSystemPath(p string) bool {
	p = filepath.Clean(p)
	if p == "/" {
		return true
	}
	for _, pre := range systemPrefixes {
		if p == pre || strings.HasPrefix(p, pre+"/") {
			return true
		}
	}
	return false
}

func isDevicePath(p string) bool {
	if strings.HasPrefix(filepath.Clean(p), "/dev/") {
		return true
	}
	if fi, err := os.Stat(p); err == nil && fi.Mode()&os.ModeDevice != 0 {
		return true
	}
	return false
}

func worldWritable(mode string) bool {
	return strings.Contains(mode, "777") ||
		strings.Contains(mode, "o+w") ||
		strings.Contains(mode, "a+w")
}
