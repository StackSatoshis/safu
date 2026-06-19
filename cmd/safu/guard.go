package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/StackSatoshis/safu/internal/audit"
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
	forceMalicious := fs.Bool("force-malicious", false, "override a confirmed-malicious package block (typed explicitly)")
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

	logger := safulog.New(config.Expand(cfg.Log.Dir), cfg.Log.ActivityRetentionDays)
	if cfg.Log.Enabled {
		_ = logger.Trim()
	}

	// Package-audit interception (§5.1): install commands route to the auditor.
	if _, pkgs, isInstall := guard.ParseInstall(*asName, argv, env.Cwd); isInstall {
		return auditInstall(cfg, pkgs, *asName, argv, *forceMalicious, *force, *yes, logger)
	}

	cmd := guard.Parse(*asName, argv, env)
	dec := guard.Classify(cmd, cfg.Guard.Level, env)

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

// auditInstall audits the packages of an install command and applies the
// verdict through the shell-hook exit-code contract: 0 approve (the shell runs
// the real install) or ExitBlock. It never performs the install itself.
func auditInstall(cfg config.Config, pkgs []audit.Package, name string, argv []string, forceMalicious, force, yes bool, logger *safulog.Logger) int {
	cmdline := strings.TrimSpace(name + " " + strings.Join(argv, " "))

	if !cfg.Audit.Enabled {
		return 0 // auditing disabled: approve
	}
	if cfg.Network.Offline {
		fmt.Fprintln(os.Stderr, "safu: offline — skipping package audit")
		return 0
	}
	if len(pkgs) == 0 {
		return 0 // nothing to audit (e.g. `npm install` from package.json)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	verdicts, err := audit.New(nil, cfg.AuditConfig()).Audit(ctx, pkgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "safu: audit error (%v); proceeding unaudited\n", err)
		return 0 // fail open (§5.5)
	}

	for _, v := range verdicts {
		printAuditVerdict(v)
		if cfg.Log.Enabled {
			_ = logger.Append(safulog.Event{
				Kind: safulog.KindAudit, Command: cmdline, Risk: string(v.Level),
				Targets: []string{v.Package.Name},
				Detail:  map[string]any{"ecosystem": string(v.Package.Ecosystem), "reasons": reasonCodes(v)},
			})
		}
	}

	risk, malicious := guard.SummarizeAudit(verdicts)

	if malicious && !forceMalicious {
		fmt.Fprintln(os.Stderr, "safu: BLOCKED — confirmed-malicious package(s) detected.")
		fmt.Fprintln(os.Stderr, "  override only with the typed flag --force-malicious")
		return shell.ExitBlock
	}
	if malicious {
		fmt.Fprintln(os.Stderr, "safu: proceeding past malicious package(s) due to --force-malicious")
		return 0
	}

	action := guard.Decide(guard.Decision{Risk: risk}, guard.Options{
		Force: force, Yes: yes, Interactive: isInteractive(),
	}, ttyPrompter{})
	if action == guard.BlockIt {
		fmt.Fprintln(os.Stderr, "safu: install not confirmed")
		return shell.ExitBlock
	}
	return 0
}

func printAuditVerdict(v audit.Verdict) {
	coord := v.Package.Name
	if v.Package.Version != "" {
		coord += "@" + v.Package.Version
	}
	fmt.Fprintf(os.Stderr, "safu audit: %s [%s] %s\n", coord, v.Package.Ecosystem, strings.ToUpper(string(v.Level)))
	if v.Unverified {
		fmt.Fprintln(os.Stderr, "  (unverified — a source could not be reached)")
	}
	for _, r := range v.Reasons {
		fmt.Fprintf(os.Stderr, "  - %s\n", r.Detail)
	}
}

func reasonCodes(v audit.Verdict) []string {
	out := make([]string, 0, len(v.Reasons))
	for _, r := range v.Reasons {
		out = append(out, r.Code)
	}
	return out
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

// isInteractive reports whether we have a real terminal on both stdin and
// stdout, so it is safe to prompt or launch a TUI. (A char-device check alone
// is wrong: /dev/null is a char device.)
func isInteractive() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
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
