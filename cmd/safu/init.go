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
	if err := fs.Parse(args); err != nil {
		return err
	}

	// 1. Write the default config.toml (safu's own dir — never the shell).
	path, err := config.Path()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr == nil && !*force {
		fmt.Printf("config already exists: %s (use --force to overwrite)\n", path)
	} else {
		if err := config.WriteDefault(path); err != nil {
			return err
		}
		fmt.Printf("wrote default config: %s\n", path)
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

	// --enable-nav: flip navigation.enabled in the file (env-free read so we
	// don't bake env overrides into the saved config) before generating hooks.
	if *enableNav {
		fileCfg, err := config.ReadFile(path)
		if err != nil {
			return err
		}
		if !fileCfg.Navigation.Enabled {
			fileCfg.Navigation.Enabled = true
			if err := config.Write(fileCfg, path); err != nil {
				return err
			}
			fmt.Println("enabled smart navigation (navigation.enabled = true)")
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
			fmt.Printf("updated %s\n", rc)
			if _, statErr := os.Stat(backup); statErr == nil {
				fmt.Printf("backup: %s\n", backup)
			}
		} else {
			fmt.Printf("%s already has safu integration — no change\n", rc)
		}
		fmt.Printf("Restart your shell or run `source %s` to activate.\n", rc)
		return nil
	}

	fmt.Printf("\n# Add the following to your %s rc file (or re-run with --write-rc):\n\n", sh)
	fmt.Println(snippet)
	return nil
}

// configCmd implements the non-interactive `safu config` views. The
// interactive wizard arrives with the TUI slice.
func configCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: safu config <show|path>")
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
