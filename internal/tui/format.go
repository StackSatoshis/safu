package tui

import (
	"fmt"
	"time"
)

// HumanAge renders a timestamp relative to now using the fzf/skim convention
// (SPEC.md §8.2): absolute time for today, then Nd / Nw, then an absolute date
// for older entries.
func HumanAge(t, now time.Time) string {
	if sameDay(t, now) {
		return "today " + t.Format("15:04")
	}
	days := calendarDaysBetween(t, now)
	switch {
	case days < 7:
		return fmt.Sprintf("%dd", days)
	case days < 56:
		return fmt.Sprintf("%dw", days/7)
	default:
		return t.Format("2006-01-02")
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// calendarDaysBetween counts whole calendar days from t to now (>= 0).
func calendarDaysBetween(t, now time.Time) int {
	ty, tm, td := t.Date()
	ny, nm, nd := now.Date()
	tMidnight := time.Date(ty, tm, td, 0, 0, 0, 0, t.Location())
	nMidnight := time.Date(ny, nm, nd, 0, 0, 0, 0, now.Location())
	days := int(nMidnight.Sub(tMidnight).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return days
}
