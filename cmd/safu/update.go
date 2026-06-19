package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/StackSatoshis/safu/internal/config"
	"github.com/StackSatoshis/safu/internal/update"
)

// updateCheckInterval throttles the automatic check triggered by `safu version`.
const updateCheckInterval = 24 * time.Hour

// safuDir returns ~/.safu (where the update-check cache lives).
func safuDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".safu"
	}
	return filepath.Join(home, ".safu")
}

// versionCmd prints the version and, unless disabled, surfaces a throttled
// (once-a-day) update notice on stderr so stdout stays scriptable.
func versionCmd(args []string) error {
	noCheck := false
	for _, a := range args {
		if a == "--no-update-check" {
			noCheck = true
		}
	}
	fmt.Println("safu", version)

	if noCheck {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil // version must never fail on config issues
	}
	if !cfg.Network.UpdateCheck || cfg.Network.Offline {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, _, _ := update.New(safuDir()).Maybe(ctx, updateCheckInterval) // errors ignored (fail open)
	if update.IsNewer(st.LatestVersion, version) {
		fmt.Fprintf(os.Stderr, "\nA newer safu is available: %s (you have %s)\n  %s\n",
			st.LatestVersion, version, update.ReleasesURL)
	}
	return nil
}

// updateCheckCmd is the explicit `safu update-check`: it always hits the
// network (honoring offline and the opt-out, which --force overrides) and
// reports the result.
func updateCheckCmd(args []string) error {
	fs := flag.NewFlagSet("update-check", flag.ContinueOnError)
	force := fs.Bool("force", false, "check even if update checks are disabled in config")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	if cfg.Network.Offline {
		fmt.Println("offline mode is on — update check skipped")
		return nil
	}
	if !cfg.Network.UpdateCheck && !*force {
		fmt.Println("update checks are disabled (network.update_check = false); use --force to check anyway")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	latest, err := update.New(safuDir()).Fetch(ctx)
	if err != nil {
		return fmt.Errorf("%w", err)
	}
	if update.IsNewer(latest, version) {
		fmt.Printf("update available: %s (you have %s)\n  %s\n", latest, version, update.ReleasesURL)
	} else {
		fmt.Printf("up to date (latest %s, you have %s)\n", latest, version)
	}
	return nil
}
