package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/StackSatoshis/safu/internal/audit"
)

// auditCmd implements `safu audit <ecosystem> <pkg>[@version]`, a thin
// hand-testing wrapper over the audit engine. It is NOT the guard's
// install-interception path (that's a later slice).
func auditCmd(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: safu audit <pypi|npm|crates|brew> <pkg>[@version]")
	}

	eco, err := parseEcosystem(args[0])
	if err != nil {
		return err
	}
	name, version := parseNameVersion(args[1])

	pkg := audit.Package{Name: name, Ecosystem: eco, Version: version}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	a := audit.New(nil, audit.DefaultConfig())
	verdicts, err := a.Audit(ctx, []audit.Package{pkg})
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	for _, v := range verdicts {
		printVerdict(v)
	}
	return nil
}

func parseEcosystem(s string) (audit.Ecosystem, error) {
	switch strings.ToLower(s) {
	case "pypi", "pip", "python":
		return audit.PyPI, nil
	case "npm", "node", "pnpm", "yarn":
		return audit.NPM, nil
	case "crates", "cargo", "rust":
		return audit.Crates, nil
	case "brew", "homebrew":
		return audit.Homebrew, nil
	default:
		return "", fmt.Errorf("unknown ecosystem %q (want pypi|npm|crates|brew)", s)
	}
}

// parseNameVersion splits "name@version" into its parts. A leading '@' (npm
// scopes like @scope/name) is preserved; only a later '@' separates version.
func parseNameVersion(s string) (name, version string) {
	if i := strings.LastIndex(s, "@"); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func printVerdict(v audit.Verdict) {
	label := strings.ToUpper(string(v.Level))
	coord := v.Package.Name
	if v.Package.Version != "" {
		coord += "@" + v.Package.Version
	}
	fmt.Printf("%s  [%s]  %s\n", coord, v.Package.Ecosystem, label)
	if v.Blocked {
		fmt.Println("  ** BLOCKED: confirmed malicious — override only with --force-malicious **")
	}
	if v.Unverified {
		fmt.Println("  (unverified: at least one source could not be reached)")
	}
	if len(v.Reasons) == 0 {
		fmt.Println("  no risk signals")
		return
	}
	for _, r := range v.Reasons {
		fmt.Printf("  - %s\n", r.Detail)
	}
}
