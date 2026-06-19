package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/StackSatoshis/safu/internal/config"
	"github.com/StackSatoshis/safu/internal/guard"
	safulog "github.com/StackSatoshis/safu/internal/log"
	"github.com/StackSatoshis/safu/internal/shell"
)

// guardCmd implements `safu guard --as <name> [flags] -- <argv>`. It returns a
// process exit code following the shell-hook contract (internal/shell):
// 0 approve, ExitHandled already-done, ExitBlock blocked, anything else =
// fail-open. On any internal error we return a non-(10/11) code so the wrapper
// falls through to the real command (invariant #3).
func guardCmd(args []string) int {
	fs := flag.NewFlagSet("guard", flag.ContinueOnError)
	asName := fs.String("as", "", "the command safu is standing in for")
	force := fs.Bool("force", false, "override a block (typed explicitly)")
	yes := fs.Bool("yes", false, "auto-confirm a warning")
	if err := fs.Parse(args); err != nil {
		return 1 // fail open
	}
	if *asName == "" {
		fmt.Fprintln(os.Stderr, "safu guard: --as <name> is required")
		return 1 // fail open
	}
	argv := argsAfterSeparator(fs.Args())

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "safu: %v\n", err)
		cfg = config.Default()
	}

	// Kill switches: pass through silently.
	if cfg.Guard.Level == "off" {
		return 0
	}

	env, err := guard.CurrentEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "safu: %v\n", err)
		return 1 // fail open
	}

	cmd := guard.Parse(*asName, argv, env)
	dec := guard.Classify(cmd, cfg.Guard.Level, env)

	logger := safulog.New(config.Expand(cfg.Log.Dir), cfg.Log.ActivityRetentionDays)
	if cfg.Log.Enabled {
		_ = logger.Trim()
	}

	if dec.Risk != guard.Safe {
		printDecision(dec)
	}

	action := guard.Decide(dec, guard.Options{
		Force:       *force,
		Yes:         *yes,
		Interactive: isInteractive(),
	}, ttyPrompter{})

	cmdline := *asName + " " + strings.Join(argv, " ")

	if action == guard.BlockIt {
		fmt.Fprintf(os.Stderr, "safu: blocked `%s`\n", strings.TrimSpace(cmdline))
		if s := firstSuggestion(dec); s != "" {
			fmt.Fprintf(os.Stderr, "  suggestion: %s\n", s)
		}
		fmt.Fprintln(os.Stderr, "  override with --force if you are sure")
		if cfg.Log.Enabled {
			_ = logger.Append(safulog.Event{
				Kind: safulog.KindBlock, Command: cmdline, Risk: string(dec.Risk),
				Targets: targetPaths(cmd), Detail: findingDetail(dec),
			})
		}
		return shell.ExitBlock
	}

	// Approved. Soft-delete handling: if this is a delete and soft-delete is on,
	// safu performs the move itself and tells the shell not to run real rm.
	if cmd.IsDelete() && cfg.Guard.SoftDelete && hasExistingTarget(cmd) {
		trashDir := config.Expand(cfg.Guard.TrashDir)
		_, _ = guard.Sweep(trashDir, cfg.Guard.TrashRetentionDays, env.Now())
		m, err := guard.Trash(cmd.Targets, trashDir, cmdline, env.Now())
		if err != nil {
			fmt.Fprintf(os.Stderr, "safu: soft-delete failed (%v); proceeding with real command\n", err)
			return 0 // fail open: let the real rm run
		}
		fmt.Fprintf(os.Stderr, "safu: moved %d item(s) to trash — `safu undo` to restore\n", len(m.Entries))
		if cfg.Log.Enabled {
			_ = logger.Append(safulog.Event{
				Kind: safulog.KindSoftDelete, Command: cmdline, Targets: targetPaths(cmd),
				Detail: map[string]any{"op_id": m.OpID, "count": len(m.Entries)},
			})
		}
		return shell.ExitHandled
	}

	if dec.Risk == guard.Warn && cfg.Log.Enabled {
		_ = logger.Append(safulog.Event{
			Kind: safulog.KindWarnProceed, Command: cmdline, Risk: string(dec.Risk),
			Targets: targetPaths(cmd),
		})
	}
	return 0
}

func argsAfterSeparator(rest []string) []string {
	// flag.Args() already drops everything up to the first non-flag, but the
	// shell hook passes an explicit "--"; honor it if present.
	for i, a := range rest {
		if a == "--" {
			return rest[i+1:]
		}
	}
	return rest
}

func printDecision(d guard.Decision) {
	for _, f := range d.Findings {
		fmt.Fprintf(os.Stderr, "safu: [%s] %s\n", f.Risk, f.Reason)
	}
	if d.Preview != nil {
		p := d.Preview
		approx := ""
		if p.Capped {
			approx = "≥"
		}
		fmt.Fprintf(os.Stderr, "  preview: %s%d files, %d dirs, %s\n", approx, p.Files, p.Dirs, humanBytes(p.Bytes))
		for _, w := range p.Warnings {
			fmt.Fprintf(os.Stderr, "  ! %s\n", w)
		}
	}
}

func firstSuggestion(d guard.Decision) string {
	for _, f := range d.Findings {
		if f.Suggestion != "" {
			return f.Suggestion
		}
	}
	return ""
}

func findingDetail(d guard.Decision) map[string]any {
	rules := make([]string, 0, len(d.Findings))
	for _, f := range d.Findings {
		rules = append(rules, f.Rule)
	}
	return map[string]any{"rules": rules}
}

func targetPaths(c guard.Command) []string {
	out := make([]string, 0, len(c.Targets))
	for _, t := range c.Targets {
		out = append(out, t.Abs)
	}
	return out
}

func hasExistingTarget(c guard.Command) bool {
	for _, t := range c.Targets {
		if t.Exists {
			return true
		}
	}
	return false
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// isInteractive reports whether stdin is a terminal (so we can prompt).
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// ttyPrompter reads a y/N answer from the controlling terminal (/dev/tty) so it
// works even when stdin is the data being piped to the wrapped command.
type ttyPrompter struct{}

func (ttyPrompter) Confirm(prompt string) bool {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	defer tty.Close()
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	sc := bufio.NewScanner(tty)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}
