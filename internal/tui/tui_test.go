package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/StackSatoshis/safu/internal/config"
	safulog "github.com/StackSatoshis/safu/internal/log"
)

func TestApplyAnswersRoundTrip(t *testing.T) {
	cur := config.Default()
	a := answersFrom(cur)
	got := applyAnswers(config.Default(), a)
	// answersFrom + applyAnswers over defaults should reproduce the managed
	// fields exactly.
	if got.Guard.Level != cur.Guard.Level || got.Audit.Enabled != cur.Audit.Enabled {
		t.Errorf("round-trip changed managed fields: %+v", got)
	}
}

func TestApplyAnswersOverlays(t *testing.T) {
	cur := config.Default()
	// A field the wizard does not manage, to confirm it's preserved.
	cur.Audit.NewPackageAgeDays = 45

	a := answersFrom(cur)
	a.Level = "paranoid"
	a.SoftDelete = false
	a.NavOn = true
	a.Offline = true
	a.Wrapped = []string{"rm", "dd"}

	got := applyAnswers(cur, a)
	if got.Guard.Level != "paranoid" {
		t.Errorf("level = %q, want paranoid", got.Guard.Level)
	}
	if got.Guard.SoftDelete {
		t.Error("soft_delete should be false")
	}
	if !got.Navigation.Enabled {
		t.Error("navigation should be enabled")
	}
	if !got.Network.Offline {
		t.Error("offline should be true")
	}
	if len(got.Guard.Wrapped) != 2 {
		t.Errorf("wrapped = %v, want [rm dd]", got.Guard.Wrapped)
	}
	if got.Audit.NewPackageAgeDays != 45 {
		t.Errorf("unmanaged field clobbered: NewPackageAgeDays = %d", got.Audit.NewPackageAgeDays)
	}
	// Result must still validate.
	if err := got.Validate(); err != nil {
		t.Errorf("wizard output invalid: %v", err)
	}
}

func TestApplyAnswersHistoryDays(t *testing.T) {
	cur := config.Default()
	a := answersFrom(cur)

	a.HistoryDays = "30"
	if got := applyAnswers(cur, a).Log.HistoryRetentionDays; got != 30 {
		t.Errorf("history retention = %d, want 30", got)
	}
	// Invalid / non-positive input leaves the current value untouched.
	a.HistoryDays = "nope"
	if got := applyAnswers(cur, a).Log.HistoryRetentionDays; got != cur.Log.HistoryRetentionDays {
		t.Errorf("invalid days changed retention to %d", got)
	}
	a.HistoryDays = "0"
	if got := applyAnswers(cur, a).Log.HistoryRetentionDays; got != cur.Log.HistoryRetentionDays {
		t.Errorf("zero days changed retention to %d", got)
	}
}

func TestNewRowAndFilter(t *testing.T) {
	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	r := newRow(safulog.Event{
		Time: now, Kind: safulog.KindBlock, Command: "rm -rf /",
	}, now)
	if r.age != "today 15:00" || r.kind != "block" || r.command != "rm -rf /" {
		t.Errorf("newRow = %+v", r)
	}
	if r.match != "block rm -rf /" {
		t.Errorf("match text = %q", r.match)
	}

	rows := []row{
		{kind: "block", command: "rm -rf /", match: "block rm -rf /"},
		{kind: "soft_delete", command: "rm junk", match: "soft_delete rm junk"},
		{kind: "undo", command: "npm install lodash", match: "undo npm install lodash"},
	}
	// Empty query: unchanged.
	if got := filterRows(rows, ""); len(got) != 3 {
		t.Errorf("empty filter returned %d rows, want 3", len(got))
	}
	// "npm" should match only the undo row.
	got := filterRows(rows, "npm")
	if len(got) != 1 || got[0].kind != "undo" {
		t.Errorf("filter npm = %+v, want the undo row", got)
	}
}

func TestHistoryBrowserBuildAndFilter(t *testing.T) {
	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	entries := []safulog.HistoryEntry{
		{Command: "ls", Time: now, Exit: 0},
		{Command: "git push", Time: now, Exit: 1},
		{Command: "ls", Time: now, Exit: 0}, // dup
	}
	rows := buildHistRows(entries, now)
	// Deduped to 2, newest-first (latest "ls" last in dedup → first newest).
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (deduped)", len(rows))
	}
	if rows[0].command != "ls" {
		t.Errorf("newest-first ordering wrong: %+v", rows)
	}
	// failed flag carried.
	var sawFailed bool
	for _, r := range rows {
		if r.command == "git push" && r.failed {
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Error("git push should be marked failed (exit 1)")
	}

	got := filterHistRows(rows, "push")
	if len(got) != 1 || got[0].command != "git push" {
		t.Errorf("filter push = %+v", got)
	}
}

func TestHistoryModelSelect(t *testing.T) {
	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	m := newHistoryModel([]safulog.HistoryEntry{
		{Command: "git status", Time: now},
		{Command: "npm test", Time: now},
	}, now)
	// Enter selects the row under the cursor.
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := mm.(historyModel).selected; got == "" {
		t.Error("enter should set a selection")
	}
	// Esc cancels (empty selection).
	mm2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := mm2.(historyModel).selected; got != "" {
		t.Errorf("esc should cancel, got %q", got)
	}
}

// TestLogModelUpdate drives the bubbletea model's Update directly (no TTY) to
// confirm filtering, navigation, and quit wiring don't panic and behave.
func TestLogModelUpdate(t *testing.T) {
	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	events := []safulog.Event{
		{Time: now, Kind: safulog.KindBlock, Command: "rm -rf /"},
		{Time: now, Kind: safulog.KindAudit, Command: "npm install lodash"},
	}
	m := newLogModel(events, now)
	if len(m.filtered) != 2 {
		t.Fatalf("initial rows = %d, want 2", len(m.filtered))
	}

	// Type a filter; only the npm row should remain.
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("npm")})
	m = mm.(logModel)
	if len(m.filtered) != 1 || m.filtered[0].kind != safulog.KindAudit {
		t.Errorf("after filter: %+v", m.filtered)
	}

	// Esc should request quit.
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc}); cmd == nil {
		t.Error("esc should return a quit command")
	}

	// View should not panic and should render the filter line.
	if out := m.View(); out == "" {
		t.Error("View returned empty")
	}
}
