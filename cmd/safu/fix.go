package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/StackSatoshis/safu/internal/fix"
)

// fixCmd implements `safu fix`. It reads the previous command's STORED output
// (script via args, exit via --exit, stderr via stdin) and prints corrections.
// It NEVER executes anything (invariant #4) — the shell function runs the
// chosen correction after confirmation.
//
//	--first   print only the top correction (used by the fix/wtf shell function)
//	--list    print all ranked corrections (default; for direct human use)
//	--exit N  the previous command's exit code
//	--rerun   allow rules that need fresh output (read-only commands only otherwise)
func fixCmd(args []string) error {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	first := fs.Bool("first", false, "print only the top correction")
	exit := fs.Int("exit", 0, "exit code of the previous command")
	rerun := fs.Bool("rerun", false, "allow rules that require re-running read-only commands")
	if err := fs.Parse(args); err != nil {
		return err
	}

	script := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if script == "" {
		return fmt.Errorf("usage: safu fix [--first] [--exit N] -- <command> (stderr on stdin)")
	}

	// stderr is piped in by the shell function; read it only when stdin is not
	// a terminal (so direct interactive use doesn't block).
	var stderr string
	if !term.IsTerminal(os.Stdin.Fd()) {
		if b, err := io.ReadAll(os.Stdin); err == nil {
			stderr = string(b)
		}
	}

	cmd := fix.Command{Script: script, Stderr: stderr, ExitCode: *exit}
	corrections := fix.DefaultEngine().Correct(cmd, *rerun)
	if len(corrections) == 0 {
		return errSilent // exit 1, no output: the shell function prints its own notice
	}

	if *first {
		fmt.Println(corrections[0].Command)
		return nil
	}
	for i, c := range corrections {
		fmt.Printf("%d) %s\n", i+1, c.Command)
	}
	return nil
}
