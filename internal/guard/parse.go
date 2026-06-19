package guard

import (
	"os"
	"path/filepath"
	"strings"
)

// Parse turns a command name + argv into a structured Command. Flags are
// interpreted per command; positional arguments are resolved to absolute paths
// against env.Cwd (no globbing — the shell already expanded globs; no
// execution). Path resolution failures are non-fatal (the Target simply lacks
// Exists/IsDir).
func Parse(name string, argv []string, env Env) Command {
	c := Command{Name: name, Args: argv, Options: map[string]string{}}

	switch name {
	case "rm":
		parseRm(&c, argv, env)
	case "dd":
		parseDD(&c, argv, env)
	case "mkfs":
		parseDevice(&c, argv, env)
	case "chmod":
		parseChmod(&c, argv, env)
	case "chown":
		parseChown(&c, argv, env)
	case "git":
		parseGit(&c, argv)
	default:
		// Unknown wrapped command: treat non-flag args as path targets.
		for _, a := range argv {
			if !strings.HasPrefix(a, "-") {
				c.Targets = append(c.Targets, resolveTarget(a, env))
			}
		}
	}
	return c
}

// isFlag reports whether arg is an option (and not the "--" separator or a
// bare "-").
func isFlag(arg string) bool {
	return strings.HasPrefix(arg, "-") && arg != "-" && arg != "--"
}

func parseRm(c *Command, argv []string, env Env) {
	afterDD := false
	for _, a := range argv {
		if a == "--" {
			afterDD = true
			continue
		}
		if !afterDD && isFlag(a) {
			if strings.HasPrefix(a, "--") {
				switch a {
				case "--recursive":
					c.Recursive = true
				case "--force":
					c.Force = true
				}
				continue
			}
			// short flags, possibly combined (-rf)
			for _, ch := range a[1:] {
				switch ch {
				case 'r', 'R':
					c.Recursive = true
				case 'f':
					c.Force = true
				}
			}
			continue
		}
		c.Targets = append(c.Targets, resolveTarget(a, env))
	}
}

// parseDD reads dd's key=value operands; the write target is `of=`.
func parseDD(c *Command, argv []string, env Env) {
	for _, a := range argv {
		if k, v, ok := strings.Cut(a, "="); ok {
			c.Options[k] = v
			if k == "of" {
				t := resolveTarget(v, env)
				c.Targets = append(c.Targets, t)
				c.Options["of_abs"] = t.Abs
			}
		}
	}
}

// parseDevice handles mkfs and similar: the device is the last positional.
func parseDevice(c *Command, argv []string, env Env) {
	for _, a := range argv {
		if !isFlag(a) && a != "--" {
			c.Targets = append(c.Targets, resolveTarget(a, env))
		}
	}
}

func parseChmod(c *Command, argv []string, env Env) {
	modeSet := false
	for _, a := range argv {
		if isFlag(a) {
			for _, ch := range strings.TrimLeft(a, "-") {
				if ch == 'R' {
					c.Recursive = true
				}
			}
			if a == "--recursive" {
				c.Recursive = true
			}
			continue
		}
		if a == "--" {
			continue
		}
		if !modeSet {
			c.Options["mode"] = a
			modeSet = true
			continue
		}
		c.Targets = append(c.Targets, resolveTarget(a, env))
	}
}

func parseChown(c *Command, argv []string, env Env) {
	ownerSet := false
	for _, a := range argv {
		if isFlag(a) {
			for _, ch := range strings.TrimLeft(a, "-") {
				if ch == 'R' {
					c.Recursive = true
				}
			}
			if a == "--recursive" {
				c.Recursive = true
			}
			continue
		}
		if a == "--" {
			continue
		}
		if !ownerSet {
			c.Options["owner"] = a
			ownerSet = true
			continue
		}
		c.Targets = append(c.Targets, resolveTarget(a, env))
	}
}

// parseGit captures only what the force-push rule needs (no path targets).
func parseGit(c *Command, argv []string) {
	for i, a := range argv {
		if i == 0 {
			c.Options["subcommand"] = a
		}
		switch a {
		case "push":
			c.Options["subcommand"] = "push"
		case "--force", "-f":
			c.Options["push_force"] = "true"
			c.Force = true
		case "--force-with-lease":
			c.Options["push_force_with_lease"] = "true"
		}
	}
}

// resolveTarget cleans and absolutizes a path argument against env.Cwd and
// records whether it exists / is a directory. Symlinks are resolved
// best-effort for danger detection.
func resolveTarget(arg string, env Env) Target {
	t := Target{Arg: arg}

	p := arg
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = env.Home + p[1:]
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(env.Cwd, p)
	}
	t.Abs = filepath.Clean(p)
	t.Real = t.Abs

	if real, err := filepath.EvalSymlinks(t.Abs); err == nil {
		t.Real = real
	}
	if fi, err := os.Lstat(t.Abs); err == nil {
		t.Exists = true
		t.IsDir = fi.IsDir()
	}
	return t
}
