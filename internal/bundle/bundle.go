// Package bundle implements the safu config bundle (SPEC.md §11): a
// preconfigured, profile-based shell setup that ships safu already wired in,
// plus hand-authored shell options/aliases/prompt. It is a CONFIGURATION
// bundle, not a terminal emulator, and bundles no third-party binaries.
//
// Everything it installs is hand-authored shell or safu's own hooks — no
// external tools, no network — so it satisfies safu's own privacy bar by
// construction (§11.3). Installation is fully previewed, backed up, and
// reversible (including a standalone uninstaller that works if safu is broken).
package bundle

import (
	"fmt"
	"strings"

	"github.com/StackSatoshis/safu/internal/config"
	"github.com/StackSatoshis/safu/internal/shell"
)

// Profile selects how much the bundle turns on (§11.2).
type Profile string

const (
	Minimal  Profile = "minimal"
	Standard Profile = "standard"
	Teaching Profile = "teaching"
)

// ParseProfile validates a profile name.
func ParseProfile(s string) (Profile, error) {
	switch Profile(strings.ToLower(strings.TrimSpace(s))) {
	case Minimal:
		return Minimal, nil
	case Standard:
		return Standard, nil
	case Teaching:
		return Teaching, nil
	default:
		return "", fmt.Errorf("unknown profile %q (want minimal|standard|teaching)", s)
	}
}

// Component is one listed piece of the bundle, shown in the manifest so the
// user sees the full superset before anything is written (§11.2).
type Component struct {
	Key       string
	Title     string
	Included  bool
	Skippable bool
}

// Manifest is the fully-resolved plan: the config to write and the rc block to
// install, plus the component list for preview.
type Manifest struct {
	Profile    Profile
	Shell      shell.Shell
	Config     config.Config
	Components []Component
	RCBlock    string
}

// Build resolves a profile + skip set into a Manifest. base is the user's
// current config (overlaid, so unmanaged fields are preserved). Only bash and
// zsh are supported; fish is deferred.
func Build(profile Profile, sh shell.Shell, base config.Config, skip map[string]bool) (Manifest, error) {
	if sh != shell.Bash && sh != shell.Zsh {
		return Manifest{}, fmt.Errorf("the bundle supports bash and zsh (got %s)", sh)
	}

	cfg := base
	// Core (all profiles): standard guard + soft-delete; auditor on.
	cfg.Guard.Level = "standard"
	cfg.Guard.SoftDelete = true
	cfg.Audit.Enabled = true

	includeFix := !skip["fix"]
	includeOpts := !skip["shell-options"]
	aboveCore := profile != Minimal
	includeNav := aboveCore && !skip["nav"]
	includeAliases := aboveCore && !skip["aliases"]
	includePrompt := aboveCore && !skip["prompt"]

	cfg.Fix.Enabled = includeFix
	cfg.Navigation.Enabled = includeNav

	comps := []Component{
		{Key: "guard", Title: "Command guard + shell integration", Included: true, Skippable: false},
		{Key: "soft-delete", Title: "Soft-delete + undo", Included: true, Skippable: false},
		{Key: "audit", Title: "Package auditor (pre-enabled)", Included: true, Skippable: false},
		{Key: "shell-options", Title: "Sane shell options (history dedup, noclobber)", Included: includeOpts, Skippable: true},
		{Key: "fix", Title: "Correction helper fix/wtf (captures stderr locally)", Included: includeFix, Skippable: true},
	}
	if aboveCore {
		comps = append(comps,
			Component{Key: "nav", Title: "Smart navigation (safu z)", Included: includeNav, Skippable: true},
			Component{Key: "aliases", Title: "Curated aliases (ll, la, gs, gd, mkcd…)", Included: includeAliases, Skippable: true},
			Component{Key: "prompt", Title: "Clean git-aware prompt", Included: includePrompt, Skippable: true},
		)
	}
	if profile == Teaching {
		comps = append(comps, Component{Key: "teaching", Title: "Beginner hints + cheatsheet alias", Included: true, Skippable: true})
	}

	// Assemble the rc block (inner content; the installer wraps it in markers).
	var b strings.Builder
	writeSection := func(s string) {
		s = strings.TrimRight(s, "\n")
		if s != "" {
			b.WriteString(s)
			b.WriteString("\n\n")
		}
	}

	guardSnip, err := shell.Snippet(sh, cfg.Guard.Wrapped)
	if err != nil {
		return Manifest{}, err
	}
	writeSection(guardSnip)

	if includeNav {
		s, err := shell.NavSnippet(sh, cfg.Navigation.Cmd)
		if err != nil {
			return Manifest{}, err
		}
		writeSection(s)
	}
	if includeFix {
		s, err := shell.FixSnippet(sh, cfg.Fix.Aliases)
		if err != nil {
			return Manifest{}, err
		}
		writeSection(s)
	}
	if includeOpts {
		writeSection(shellOptions(sh))
	}
	if includeAliases {
		writeSection(aliasesBlock(sh))
	}
	if includePrompt {
		writeSection(promptBlock(sh))
	}
	if profile == Teaching {
		writeSection(teachingBlock(sh))
	}

	return Manifest{
		Profile:    profile,
		Shell:      sh,
		Config:     cfg,
		Components: comps,
		RCBlock:    strings.TrimRight(b.String(), "\n") + "\n",
	}, nil
}
