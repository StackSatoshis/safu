package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/StackSatoshis/safu/internal/bundle"
	"github.com/StackSatoshis/safu/internal/config"
	"github.com/StackSatoshis/safu/internal/shell"
)

// bundleOpts are the inputs to an install, shared by `safu bundle install` and
// `safu init --bundle`.
type bundleOpts struct {
	shellName string
	profile   string
	skip      string
	yes       bool
	dryRun    bool
}

// bundleCmd routes `safu bundle <install|uninstall>`.
func bundleCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: safu bundle <install|uninstall> [flags]")
	}
	switch args[0] {
	case "install":
		return parseAndInstall(args[1:])
	case "uninstall":
		return bundleUninstall(args[1:])
	default:
		return fmt.Errorf("unknown bundle subcommand %q (want install|uninstall)", args[0])
	}
}

func parseAndInstall(args []string) error {
	fs := flag.NewFlagSet("bundle install", flag.ContinueOnError)
	var o bundleOpts
	fs.StringVar(&o.shellName, "shell", "", "shell to target: bash|zsh (default: autodetect)")
	fs.StringVar(&o.profile, "profile", "standard", "minimal|standard|teaching")
	fs.StringVar(&o.skip, "skip", "", "comma-separated components to skip (nav,aliases,prompt,fix,shell-options)")
	fs.BoolVar(&o.yes, "yes", false, "install without the confirmation prompt")
	fs.BoolVar(&o.dryRun, "dry-run", false, "print the manifest and the rc block, write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runBundleInstall(o)
}

func runBundleInstall(o bundleOpts) error {
	profile, err := bundle.ParseProfile(o.profile)
	if err != nil {
		return err
	}
	var sh shell.Shell
	if o.shellName != "" {
		sh, err = shell.Parse(o.shellName)
	} else {
		sh, err = shell.Detect()
	}
	if err != nil {
		return fmt.Errorf("%w; pass --shell bash|zsh", err)
	}

	cfgPath, err := config.Path()
	if err != nil {
		return err
	}
	base, err := config.ReadFile(cfgPath) // env-free: don't bake env overrides
	if err != nil {
		base = config.Default()
	}

	skip := map[string]bool{}
	for _, s := range strings.Split(o.skip, ",") {
		if s = strings.TrimSpace(s); s != "" {
			skip[s] = true
		}
	}

	m, err := bundle.Build(profile, sh, base, skip)
	if err != nil {
		return err
	}

	rc, err := shell.DefaultRC(sh)
	if err != nil {
		return err
	}
	printManifest(m, rc, o.dryRun)
	if o.dryRun {
		return nil
	}

	if !o.yes {
		if !isInteractive() {
			return fmt.Errorf("refusing to modify %s without confirmation; re-run in a terminal or with --yes", rc)
		}
		if !(ttyPrompter{}).Confirm("Install this bundle?") {
			fmt.Println("cancelled — nothing written")
			return nil
		}
	}

	home, _ := os.UserHomeDir()
	res, err := bundle.Install(m, bundle.Paths{
		RC:      rc,
		Config:  cfgPath,
		SafuDir: filepath.Join(home, ".safu"),
	}, time.Now())
	if err != nil {
		return err
	}

	fmt.Printf("installed the %s bundle into %s\n", m.Profile, rc)
	if res.RCBackup != "" {
		fmt.Printf("  rc backup:      %s\n", res.RCBackup)
	}
	if res.ConfigBackup != "" {
		fmt.Printf("  config backup:  %s\n", res.ConfigBackup)
	}
	fmt.Printf("  uninstall:      safu bundle uninstall   (or run %s)\n", res.Uninstaller)
	fmt.Printf("Restart your shell or run `source %s` to activate.\n", rc)
	return nil
}

func bundleUninstall(args []string) error {
	fs := flag.NewFlagSet("bundle uninstall", flag.ContinueOnError)
	shellName := fs.String("shell", "", "shell to target: bash|zsh (default: autodetect)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var sh shell.Shell
	var err error
	if *shellName != "" {
		sh, err = shell.Parse(*shellName)
	} else {
		sh, err = shell.Detect()
	}
	if err != nil {
		return fmt.Errorf("%w; pass --shell bash|zsh", err)
	}
	rc, err := shell.DefaultRC(sh)
	if err != nil {
		return err
	}
	removed, err := bundle.Uninstall(rc)
	if err != nil {
		return err
	}
	if !removed {
		fmt.Printf("no safu bundle block found in %s\n", rc)
		return nil
	}
	fmt.Printf("removed the safu bundle block from %s\n", rc)
	fmt.Println("(a pre-bundle backup may remain alongside it; your config.toml is unchanged)")
	return nil
}

func printManifest(m bundle.Manifest, rc string, dryRun bool) {
	fmt.Printf("safu bundle — profile %q (%s)\n", m.Profile, m.Shell)
	fmt.Printf("target rc: %s\n\n", rc)
	fmt.Println("components:")
	for _, c := range m.Components {
		mark := "✓"
		if !c.Included {
			mark = "·"
		}
		skip := ""
		if c.Skippable {
			skip = "  (--skip " + c.Key + ")"
		}
		fmt.Printf("  %s %s%s\n", mark, c.Title, skip)
	}
	fmt.Printf("\nconfig: level=%s soft_delete=%v audit=%v nav=%v fix=%v\n",
		m.Config.Guard.Level, m.Config.Guard.SoftDelete, m.Config.Audit.Enabled,
		m.Config.Navigation.Enabled, m.Config.Fix.Enabled)
	fmt.Println("Existing dotfiles are backed up (timestamped) before any change.")
	if dryRun {
		fmt.Println("\n--- rc block that would be added ---")
		fmt.Println(m.RCBlock)
	}
}
