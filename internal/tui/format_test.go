package tui

import (
	"testing"
	"time"
)

func TestHumanAge(t *testing.T) {
	now := time.Date(2026, 6, 19, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"today", time.Date(2026, 6, 19, 9, 22, 0, 0, time.UTC), "today 09:22"},
		{"3 days", time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC), "3d"},
		{"2 weeks", time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC), "2w"},
		{"old date", time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), "2026-01-01"},
	}
	for _, c := range cases {
		if got := HumanAge(c.t, now); got != c.want {
			t.Errorf("%s: HumanAge = %q, want %q", c.name, got, c.want)
		}
	}
}
