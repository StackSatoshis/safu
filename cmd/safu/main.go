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
  version    Print the safu version
  help       Show this help

More commands (guard, init, z, log, fix) are added per build slice; see SPEC.md.`)
}
