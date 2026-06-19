package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/StackSatoshis/safu/internal/config"
	"github.com/StackSatoshis/safu/internal/shell"
)

// initCmd implements `safu init`: writes a default config.toml if absent and
// emits the shell-integration snippet. By default it only PRINTS the snippet;
// editing the rc file requires --write-rc (invariant #6: install never
// silently touches the shell).
func initCmd(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	shellName := fs.String("shell", "", "shell to target: bash|zsh|fish (default: autodetect from $SHELL)")
	writeRC := fs.Bool("write-rc", false, "append the snippet to your shell rc file (timestamped backup first)")
	force := fs.Bool("force", false, "overwrite an existing config.toml")
	enableNav := fs.Bool("enable-nav", false, "enable smart navigation (safu z) and emit its hook")
	enableFix := fs.Bool("enable-fix", false, "enable the correction helper (safu fix/wtf) and emit its hook")
	enableHistory := fs.Bool("enable-history", false, "enable general shell history (Ctrl-R) and emit its hook")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// 1. Write the default config.toml (safu's own dir — never the shell).
	path, err := config.Path()
	if err != nil {
		return err
	}
	// Status/diagnostics go to stderr so stdout carries ONLY the shell snippet
	// (so `eval "$(safu init --shell bash)"` works cleanly).
	if _, statErr := os.Stat(path); statErr == nil && !*force {
		fmt.Fprintf(os.Stderr, "config already exists: %s (use --force to overwrite)\n", path)
	} else {
		if err := config.WriteDefault(path); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote default config: %s\n", path)
	}

	// 2. Resolve the target shell.
	var sh shell.Shell
	if *shellName != "" {
		sh, err = shell.Parse(*shellName)
	} else {
		sh, err = shell.Detect()
	}
	if err != nil {
		return fmt.Errorf("%w; pass --shell bash|zsh|fish", err)
	}

	// --enable-nav / --enable-fix: flip the opt-ins in the file (env-free read
	// so we don't bake env overrides into the saved config) before generating
	// hooks.
	if *enableNav || *enableFix || *enableHistory {
		fileCfg, err := config.ReadFile(path)
		if err != nil {
			return err
		}
		changed := false
		if *enableNav && !fileCfg.Navigation.Enabled {
			fileCfg.Navigation.Enabled = true
			fmt.Fprintln(os.Stderr, "enabled smart navigation (navigation.enabled = true)")
			changed = true
		}
		if *enableFix && !fileCfg.Fix.Enabled {
			fileCfg.Fix.Enabled = true
			fmt.Fprintln(os.Stderr, "enabled correction helper (fix.enabled = true)")
			changed = true
		}
		if *enableHistory && !fileCfg.Log.History {
			fileCfg.Log.History = true
			fmt.Fprintln(os.Stderr, "enabled shell history (log.history = true)")
			changed = true
		}
		if changed {
			if err := config.Write(fileCfg, path); err != nil {
				return err
			}
		}
	}

	// Use the loaded config's wrapped list (fall back to defaults if unreadable).
	cfg, loadErr := config.Load()
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "warning: using defaults (%v)\n", loadErr)
		cfg = config.Default()
	}
	snippet, err := shell.Snippet(sh, cfg.Guard.Wrapped)
	if err != nil {
		return err
	}
	// Append the navigation hook when smart navigation is enabled.
	if cfg.Navigation.Enabled {
		navSnippet, err := shell.NavSnippet(sh, cfg.Navigation.Cmd)
		if err != nil {
			return err
		}
		snippet = snippet + "\n" + navSnippet
	}
	// Append the correction-helper hook when enabled (fish not yet supported).
	if cfg.Fix.Enabled {
		fixSnippet, err := shell.FixSnippet(sh, cfg.Fix.Aliases)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		} else {
			snippet = snippet + "\n" + fixSnippet
		}
	}
	// Append the shell-history hook when enabled (fish not yet supported).
	if cfg.Log.History {
		histSnippet, err := shell.HistorySnippet(sh)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		} else {
			snippet = snippet + "\n" + histSnippet
		}
	}

	// 3. Print, or (opt-in) write to the rc file with a backup.
	if *writeRC {
		rc, err := shell.DefaultRC(sh)
		if err != nil {
			return err
		}
		backup := fmt.Sprintf("%s.safu-backup-%s", rc, time.Now().Format("20060102-150405"))
		changed, err := shell.InstallToRC(rc, snippet, backup)
		if err != nil {
			return err
		}
		if changed {
			fmt.Fprintf(os.Stderr, "updated %s\n", rc)
			if _, statErr := os.Stat(backup); statErr == nil {
				fmt.Fprintf(os.Stderr, "backup: %s\n", backup)
			}
		} else {
			fmt.Fprintf(os.Stderr, "%s already has safu integration — no change\n", rc)
		}
		fmt.Fprintf(os.Stderr, "Restart your shell or run `source %s` to activate.\n", rc)
		return nil
	}

	// Print-only: guidance to stderr, the snippet alone to stdout so
	// `eval "$(safu init --shell bash)"` ingests clean shell code.
	fmt.Fprintf(os.Stderr, "# Add to your %s rc file, or eval directly, or re-run with --write-rc:\n", sh)
	fmt.Println(snippet)
	return nil
}

// configCmd implements the non-interactive `safu config` views. The
// interactive wizard arrives with the TUI slice.
func configCmd(args []string) error {
	if len(args) == 0 {
		// No subcommand: launch the interactive wizard (TTY) — same as setup.
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
