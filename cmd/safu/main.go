// Command safu is a single, statically-linked, safer/smarter shell helper.
// See SPEC.md for the authoritative design. Subcommands are added per build
// slice (see SPEC.md §13 for build order); this entry point currently only
// reports its version and usage.
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "safu:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println("safu", version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	case "audit":
		return auditCmd(args[1:])
	case "init":
		return initCmd(args[1:])
	case "config":
		return configCmd(args[1:])
	case "guard":
		// guard follows the shell-hook exit-code contract (internal/shell);
		// it owns its own exit codes, so exit directly.
		os.Exit(guardCmd(args[1:]))
		return nil
	case "undo":
		return undoCmd(args[1:])
	case "log":
		return logCmd(args[1:])
	default:
		return fmt.Errorf("unknown command %q (run `safu help`)", args[0])
	}
}

func usage() {
	fmt.Println(`safu — a safer, smarter shell

Usage:
  safu <command> [args]

Commands:
  audit      Audit a package before install (audit <pypi|npm|crates|brew> <pkg>[@version])
  guard      Guard a destructive command (used by shell hooks: guard --as <cmd> -- ...)
  undo       Restore the most recent soft-deleted operation (undo [--list])
  log        View the activity log (log [--json|--grep|--since|--clear])
  init       Write default config and print shell integration (--shell, --write-rc)
  config     Inspect config (config show | config path)
  version    Print the safu version
  help       Show this help

More commands (z, fix) are added per build slice; see SPEC.md.`)
}
