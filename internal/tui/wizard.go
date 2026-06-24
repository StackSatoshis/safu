package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/StackSatoshis/safu/internal/config"
)

// answers holds the wizard's collected values, bound to the huh form.
type answers struct {
	Level       string
	Wrapped     []string
	SoftDelete  bool
	AuditOn     bool
	NavOn       bool
	FixOn       bool
	LogOn       bool
	HistoryOn   bool
	HistoryDays string
	UpdateCheck bool
	Offline     bool
}

// answersFrom projects a Config into the wizard's editable answers.
func answersFrom(c config.Config) answers {
	return answers{
		Level:       c.Guard.Level,
		Wrapped:     append([]string(nil), c.Guard.Wrapped...),
		SoftDelete:  c.Guard.SoftDelete,
		AuditOn:     c.Audit.Enabled,
		NavOn:       c.Navigation.Enabled,
		FixOn:       c.Fix.Enabled,
		LogOn:       c.Log.Enabled,
		HistoryOn:   c.Log.History,
		HistoryDays: strconv.Itoa(c.Log.HistoryRetentionDays),
		UpdateCheck: c.Network.UpdateCheck,
		Offline:     c.Network.Offline,
	}
}

// applyAnswers overlays the wizard's answers onto a Config, leaving fields the
// wizard doesn't manage untouched. Pure and unit-tested.
func applyAnswers(c config.Config, a answers) config.Config {
	c.Guard.Level = a.Level
	c.Guard.Wrapped = append([]string(nil), a.Wrapped...)
	c.Guard.SoftDelete = a.SoftDelete
	c.Audit.Enabled = a.AuditOn
	c.Navigation.Enabled = a.NavOn
	c.Fix.Enabled = a.FixOn
	c.Log.Enabled = a.LogOn
	c.Log.History = a.HistoryOn
	if n, err := strconv.Atoi(strings.TrimSpace(a.HistoryDays)); err == nil && n > 0 {
		c.Log.HistoryRetentionDays = n
	}
	c.Network.UpdateCheck = a.UpdateCheck
	c.Network.Offline = a.Offline
	return c
}

// RunSetup runs the interactive setup/config wizard, pre-filled from cur, and
// returns the updated Config. It does not write anything — the caller persists.
// Requires a terminal; huh returns huh.ErrUserAborted if the user cancels.
func RunSetup(cur config.Config) (config.Config, error) {
	a := answersFrom(cur)

	// A single page (one huh group) — no jarring screen-clears between pages.
	// Only the high-value choices are asked; everything else keeps sensible
	// defaults (auditor on, default wrapped commands, activity log on), which
	// can be tuned later by editing config.toml.
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Protection level").
				Description("How aggressively safu guards destructive commands").
				Options(
					huh.NewOption("light — only catastrophic operations", "light"),
					huh.NewOption("standard — recommended", "standard"),
					huh.NewOption("paranoid — confirm anything outside CWD", "paranoid"),
					huh.NewOption("off — pass everything through", "off"),
				).
				Value(&a.Level),
			huh.NewConfirm().
				Title("Soft-delete: send rm to a trash you can `safu undo`?").
				Value(&a.SoftDelete),
			huh.NewConfirm().
				Title("Smart navigation: jump to dirs with `safu z`?").
				Value(&a.NavOn),
			huh.NewConfirm().
				Title("Correction helper: suggest fixes with `fix` / `wtf`?").
				Description("Captures your last command's stderr locally (no network)").
				Value(&a.FixOn),
			huh.NewConfirm().
				Title("Searchable shell history on Ctrl-R?").
				Description("Records commands you run, locally — a larger disclosure-to-disk").
				Value(&a.HistoryOn),
			huh.NewInput().
				Title("Auto-clear shell history after how many days?").
				Description("Only applies if history is on. Older entries are purged automatically; secrets are never recorded.").
				Value(&a.HistoryDays).
				Validate(func(s string) error {
					if n, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || n <= 0 {
						return fmt.Errorf("enter a positive number of days")
					}
					return nil
				}),
		),
	)

	if err := form.Run(); err != nil {
		return cur, err
	}
	return applyAnswers(cur, a), nil
}
