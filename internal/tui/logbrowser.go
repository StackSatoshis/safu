package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	safulog "github.com/StackSatoshis/safu/internal/log"
)

// row is one display line for the activity-log browser.
type row struct {
	age     string
	kind    string
	command string
	match   string // text the fuzzy filter searches
}

func newRow(e safulog.Event, now time.Time) row {
	return row{
		age:     HumanAge(e.Time, now),
		kind:    e.Kind,
		command: e.Command,
		match:   e.Kind + " " + e.Command,
	}
}

// filterRows ranks rows against query using the shared fuzzy matcher. An empty
// query returns the rows unchanged (newest-first as supplied).
func filterRows(rows []row, query string) []row {
	if strings.TrimSpace(query) == "" {
		return rows
	}
	cands := make([]string, len(rows))
	for i, r := range rows {
		cands[i] = r.match
	}
	ms := Rank(query, cands)
	out := make([]row, 0, len(ms))
	for _, m := range ms {
		out = append(out, rows[m.Index])
	}
	return out
}

var (
	styleAge    = lipgloss.NewStyle().Faint(true)
	styleKind   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	styleCursor = lipgloss.NewStyle().Reverse(true)
	styleHelp   = lipgloss.NewStyle().Faint(true)
)

type logModel struct {
	all      []row
	filtered []row
	input    textinput.Model
	cursor   int
	height   int
}

func newLogModel(events []safulog.Event, now time.Time) logModel {
	// events arrive oldest-first; show newest-first.
	rows := make([]row, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		rows = append(rows, newRow(events[i], now))
	}
	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.Prompt = "/ "
	ti.Focus()
	return logModel{all: rows, filtered: rows, input: ti, height: 20}
}

func (m logModel) Init() tea.Cmd { return textinput.Blink }

func (m logModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
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
	m.filtered = filterRows(m.all, m.input.Value())
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
	return m, cmd
}

func (m logModel) View() string {
	var b strings.Builder
	b.WriteString(m.input.View())
	b.WriteString("\n\n")

	if len(m.filtered) == 0 {
		b.WriteString(styleHelp.Render("no matching activity"))
		b.WriteString("\n")
	}

	// Window the visible rows around the cursor.
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
		line := styleAge.Render(pad(r.age, 12)) + " " +
			styleKind.Render(pad(r.kind, 12)) + " " + r.command
		if i == m.cursor {
			line = styleCursor.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("↑/↓ move · type to filter · esc to quit"))
	return b.String()
}

// RunLogBrowser launches the interactive activity-log browser.
func RunLogBrowser(events []safulog.Event, now time.Time) error {
	_, err := tea.NewProgram(newLogModel(events, now)).Run()
	return err
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
