package tui

import (
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	safulog "github.com/StackSatoshis/safu/internal/log"
)

// histRow is one display line for the history browser.
type histRow struct {
	age     string
	command string
	failed  bool
}

// buildHistRows de-duplicates (latest wins, §8.2) and orders newest-first.
func buildHistRows(entries []safulog.HistoryEntry, now time.Time) []histRow {
	deduped := safulog.DedupLatest(entries)
	rows := make([]histRow, 0, len(deduped))
	for i := len(deduped) - 1; i >= 0; i-- {
		e := deduped[i]
		rows = append(rows, histRow{age: HumanAge(e.Time, now), command: e.Command, failed: e.Exit != 0})
	}
	return rows
}

func filterHistRows(rows []histRow, query string) []histRow {
	if strings.TrimSpace(query) == "" {
		return rows
	}
	cands := make([]string, len(rows))
	for i, r := range rows {
		cands[i] = r.command
	}
	out := make([]histRow, 0)
	for _, m := range Rank(query, cands) {
		out = append(out, rows[m.Index])
	}
	return out
}

type historyModel struct {
	all      []histRow
	filtered []histRow
	input    textinput.Model
	cursor   int
	height   int
	selected string
}

func newHistoryModel(entries []safulog.HistoryEntry, now time.Time) historyModel {
	rows := buildHistRows(entries, now)
	ti := textinput.New()
	ti.Placeholder = "search history…"
	ti.Prompt = "⌕ "
	ti.Focus()
	return historyModel{all: rows, filtered: rows, input: ti, height: 20}
}

func (m historyModel) Init() tea.Cmd { return textinput.Blink }

func (m historyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.selected = ""
			return m, tea.Quit
		case tea.KeyEnter:
			if m.cursor < len(m.filtered) {
				m.selected = m.filtered[m.cursor].command
			}
			return m, tea.Quit
		case tea.KeyUp, tea.KeyCtrlP:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.KeyDown, tea.KeyCtrlN:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.filtered = filterHistRows(m.all, m.input.Value())
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
	return m, cmd
}

func (m historyModel) View() string {
	var b strings.Builder
	b.WriteString(m.input.View())
	b.WriteString("\n\n")

	visible := m.height - 4
	if visible < 1 {
		visible = 1
	}
	start := 0
	if m.cursor >= visible {
		start = m.cursor - visible + 1
	}
	end := min(len(m.filtered), start+visible)
	for i := start; i < end; i++ {
		r := m.filtered[i]
		cmd := r.command
		if r.failed {
			cmd = styleKind.Render("✗ ") + cmd
		}
		line := styleAge.Render(pad(r.age, 12)) + " " + cmd
		if i == m.cursor {
			line = styleCursor.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(styleHelp.Render("↑/↓ move · enter to pick · esc to cancel"))
	return b.String()
}

// RunHistoryBrowser shows the interactive history search and returns the chosen
// command (empty if cancelled). The UI renders to stderr so the caller (e.g. a
// Ctrl-R shell widget) can capture the selection from stdout.
func RunHistoryBrowser(entries []safulog.HistoryEntry, now time.Time) (string, error) {
	p := tea.NewProgram(newHistoryModel(entries, now), tea.WithOutput(os.Stderr))
	fm, err := p.Run()
	if err != nil {
		return "", err
	}
	return fm.(historyModel).selected, nil
}
