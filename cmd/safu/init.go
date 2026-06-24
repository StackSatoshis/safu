package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/StackSatoshis/safu/internal/config"
	"github.com/StackSatoshis/safu/internal/shell"
)

// initCmd implements `safu init`. With --write-rc it adds the shell hook to
// your rc (with a backup). At a terminal with no flags it prints short guidance
// instead of dumping shell code. With --print (or when piped/redirected) it
// emits the raw hook so `eval "$(safu init --print)"` still works.
func initCmd(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	shellName := fs.String("shell", "", "shell to target: bash|zsh|fish (default: autodetect from $SHELL)")
	writeRC := fs.Bool("write-rc", false, "add the hook to your shell rc file (timestamped backup first)")
	printRaw := fs.Bool("print", false, "print the raw shell hook to stdout (for eval or manual paste)")
	force := fs.Bool("force", false, "overwrite an existing config.toml")
	enableNav := fs.Bool("enable-nav", false, "enable smart navigation (safu z) and emit its hook")
	enableFix := fs.Bool("enable-fix", false, "enable the correction helper (safu fix/wtf) and emit its hook")
	enableHistory := fs.Bool("enable-history", false, "enable general shell history (Ctrl-R) and emit its hook")
	asBundle := fs.Bool("bundle", false, "install the preconfigured shell bundle")
	profile := fs.String("profile", "standard", "bundle profile: minimal|standard|teaching (with --bundle)")
	yes := fs.Bool("yes", false, "skip the bundle confirmation prompt (with --bundle)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// --bundle delegates to the bundle installer (SPEC.md §11.4).
	if *asBundle {
		return runBundleInstall(bundleOpts{shellName: *shellName, profile: *profile, yes: *yes})
	}

	// 1. Write the default config.toml (safu's own dir — never the shell).
	path, err := config.Path()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr == nil && !*force {
		fmt.Fprintf(os.Stderr, "config already exists: %s (use --force to overwrite)\n", path)
	} else {
		if err := config.WriteDefault(path); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote default config: %s\n", path)
	}

	// 2. Resolve the target shell.
	sh, err := resolveShell(*shellName)
	if err != nil {
		return err
	}

	// 3. Apply the opt-in toggles to the config file before building hooks.
	if err := applyEnableFlags(path, *enableNav, *enableFix, *enableHistory); err != nil {
		return err
	}

	cfg, loadErr := config.Load()
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "warning: using defaults (%v)\n", loadErr)
		cfg = config.Default()
	}
	snippet, err := buildHook(sh, cfg)
	if err != nil {
		return err
	}

	// 4. Activate, print raw, or guide.
	switch {
	case *writeRC:
		rc, err := installHook(sh, snippet)
		if err != nil {
			return err
		}
		fmt.Printf("Restart your shell or run: source %s\n", rc)
		return nil

	case *printRaw || !term.IsTerminal(os.Stdout.Fd()):
		// Raw hook for `eval "$(...)"` or redirection to a file.
		fmt.Println(snippet)
		return nil

	default:
		// Interactive, bare invocation: guide instead of dumping shell code.
		fmt.Printf(`safu is installed but not active in your shell yet.

To turn it on, run one of:

  safu setup                  interactive wizard (recommended)
  safu init --write-rc        add the hook to your %s rc (with a backup)

Or print the raw hook for eval/manual paste:  safu init --print
`, sh)
		return nil
	}
}

// resolveShell picks the target shell from a flag or autodetection.
func resolveShell(name string) (shell.Shell, error) {
	if name != "" {
		return shell.Parse(name)
	}
	sh, err := shell.Detect()
	if err != nil {
		return "", fmt.Errorf("%w; pass --shell bash|zsh|fish", err)
	}
	return sh, nil
}

// applyEnableFlags flips the opt-in toggles in the config file (env-free read so
// env overrides are not baked in).
func applyEnableFlags(path string, nav, fix, history bool) error {
	if !nav && !fix && !history {
		return nil
	}
	cfg, err := config.ReadFile(path)
	if err != nil {
		return err
	}
	changed := false
	if nav && !cfg.Navigation.Enabled {
		cfg.Navigation.Enabled, changed = true, true
		fmt.Fprintln(os.Stderr, "enabled smart navigation (navigation.enabled = true)")
	}
	if fix && !cfg.Fix.Enabled {
		cfg.Fix.Enabled, changed = true, true
		fmt.Fprintln(os.Stderr, "enabled correction helper (fix.enabled = true)")
	}
	if history && !cfg.Log.History {
		cfg.Log.History, changed = true, true
		fmt.Fprintln(os.Stderr, "enabled shell history (log.history = true)")
	}
	if changed {
		return config.Write(cfg, path)
	}
	return nil
}

// buildHook assembles the full shell snippet for the active config: the guard
// hook plus the nav/fix/history hooks for whatever is enabled.
func buildHook(sh shell.Shell, cfg config.Config) (string, error) {
	snippet, err := shell.Snippet(sh, cfg.Guard.Wrapped)
	if err != nil {
		return "", err
	}
	if cfg.Navigation.Enabled {
		s, err := shell.NavSnippet(sh, cfg.Navigation.Cmd)
		if err != nil {
			return "", err
		}
		snippet += "\n" + s
	}
	if cfg.Fix.Enabled {
		if s, err := shell.FixSnippet(sh, cfg.Fix.Aliases); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		} else {
			snippet += "\n" + s
		}
	}
	if cfg.Log.History {
		if s, err := shell.HistorySnippet(sh); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		} else {
			snippet += "\n" + s
		}
	}
	return snippet, nil
}

// installHook writes the snippet to sh's rc file with a timestamped backup,
// prints a friendly result, and returns the rc path.
func installHook(sh shell.Shell, snippet string) (string, error) {
	rc, err := shell.DefaultRC(sh)
	if err != nil {
		return "", err
	}
	backup := fmt.Sprintf("%s.safu-backup-%s", rc, time.Now().Format("20060102-150405"))
	changed, err := shell.InstallToRC(rc, snippet, backup)
	if err != nil {
		return rc, err
	}
	if changed {
		fmt.Printf("✓ added safu to %s\n", rc)
		if _, statErr := os.Stat(backup); statErr == nil {
			fmt.Printf("  (backup: %s)\n", backup)
		}
	} else {
		fmt.Printf("✓ %s already has safu integration\n", rc)
	}
	return rc, nil
}

// configCmd implements `safu config`: no args launches the wizard, otherwise
// show/path views.
func configCmd(args []string) error {
	if len(args) == 0 {
		return setupCmd(nil)
	}
	switch args[0] {
	case "path":
		p, err := config.Path()
		if err != nil {
			return err
		}
		fmt.Println(p)
		return nil
	case "show":
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		data, err := config.Render(cfg)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	default:
		return fmt.Errorf("unknown config subcommand %q (want show|path)", args[0])
	}
}
