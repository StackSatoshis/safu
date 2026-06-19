package tui

import (
	"github.com/charmbracelet/huh"

	"github.com/StackSatoshis/safu/internal/config"
)

// wrapCatalog is the set of commands offered as guard toggles in the wizard.
var wrapCatalog = []string{
	"rm", "git-push-force", "dd", "mkfs", "chmod-r", "chown-r",
	"pip", "npm", "cargo", "brew",
}

// answers holds the wizard's collected values, bound to the huh form.
type answers struct {
	Level       string
	Wrapped     []string
	SoftDelete  bool
	AuditOn     bool
	NavOn       bool
	LogOn       bool
	HistoryOn   bool
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
		LogOn:       c.Log.Enabled,
		HistoryOn:   c.Log.History,
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
	c.Log.Enabled = a.LogOn
	c.Log.History = a.HistoryOn
	c.Network.UpdateCheck = a.UpdateCheck
	c.Network.Offline = a.Offline
	return c
}

// RunSetup runs the interactive setup/config wizard, pre-filled from cur, and
// returns the updated Config. It does not write anything — the caller persists.
// Requires a terminal; huh returns huh.ErrUserAborted if the user cancels.
func RunSetup(cur config.Config) (config.Config, error) {
	a := answersFrom(cur)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Protection level").
				Description("How aggressively safu guards destructive commands").
				Options(
					huh.NewOption("off — pass everything through", "off"),
					huh.NewOption("light — only catastrophic operations", "light"),
					huh.NewOption("standard — recommended", "standard"),
					huh.NewOption("paranoid — confirm anything outside CWD", "paranoid"),
				).
				Value(&a.Level),
			huh.NewMultiSelect[string]().
				Title("Wrapped commands").
				Description("Which commands the shell hook routes through safu").
				Options(huh.NewOptions(wrapCatalog...)...).
				Value(&a.Wrapped),
			huh.NewConfirm().
				Title("Soft-delete (trash + undo) instead of permanent rm?").
				Value(&a.SoftDelete),
		),
		huh.NewGroup(
			huh.NewConfirm().Title("Audit packages before install?").Value(&a.AuditOn),
			huh.NewConfirm().Title("Enable smart navigation (safu z)?").Value(&a.NavOn),
		),
		huh.NewGroup(
			huh.NewConfirm().Title("Keep an activity log of what safu does?").Value(&a.LogOn),
			huh.NewConfirm().
				Title("Also record general shell history? (separate, off by default)").
				Description("Logs everything you type, locally — a larger disclosure-to-disk").
				Value(&a.HistoryOn),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Check for updates? (the only non-audit outbound call)").
				Value(&a.UpdateCheck),
			huh.NewConfirm().
				Title("Offline mode — disable ALL outbound calls?").
				Value(&a.Offline),
		),
	)

	if err := form.Run(); err != nil {
		return cur, err
	}
	return applyAnswers(cur, a), nil
}
