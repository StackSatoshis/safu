package main

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/StackSatoshis/safu/internal/config"
	"github.com/StackSatoshis/safu/internal/tui"
)

// setupCmd runs the interactive setup/config wizard and writes config.toml.
// It requires a terminal; the non-interactive path is `safu init` + editing
// config.toml (every TUI surface has a flag/file fallback, §8.4).
func setupCmd(args []string) error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	if !isInteractive() {
		return fmt.Errorf("setup needs a terminal; run `safu init` or edit %s", path)
	}

	// Read the file (env-free) so we prefill and persist the on-disk values,
	// not env-derived overrides.
	cur, err := config.ReadFile(path)
	if err != nil {
		cur = config.Default()
	}

	updated, err := tui.RunSetup(cur)
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("setup cancelled — no changes written")
			return nil
		}
		return err
	}
	if err := updated.Validate(); err != nil {
		return err
	}
	if err := config.Write(updated, path); err != nil {
		return err
	}
	fmt.Printf("saved %s\n", path)
	fmt.Println("run `safu init` to (re)generate your shell integration")
	return nil
}
