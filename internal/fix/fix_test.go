package fix

import (
	"strings"
	"testing"
)

func TestEnginePriorityAndDedup(t *testing.T) {
	low := Rule{Name: "low", Priority: 1, Match: func(Command) bool { return true }, Fix: func(Command) []string { return []string{"same"} }}
	high := Rule{Name: "high", Priority: 9, Match: func(Command) bool { return true }, Fix: func(Command) []string { return []string{"first", "same"} }}
	e := New([]Rule{low, high})

	got := e.Correct(Command{Script: "x"}, false)
	if len(got) != 2 {
		t.Fatalf("got %d corrections, want 2 (dedup of 'same'): %+v", len(got), got)
	}
	if got[0].Command != "first" || got[0].Rule != "high" {
		t.Errorf("highest priority should come first, got %+v", got[0])
	}
	if got[1].Command != "same" {
		t.Errorf("expected deduped 'same' second, got %+v", got[1])
	}
}

func TestEngineSkipsOriginalScript(t *testing.T) {
	r := Rule{Name: "noop", Priority: 1, Match: func(Command) bool { return true }, Fix: func(c Command) []string { return []string{c.Script} }}
	got := New([]Rule{r}).Correct(Command{Script: "git status"}, false)
	if len(got) != 0 {
		t.Errorf("a fix identical to the script should be dropped: %+v", got)
	}
}

func TestRequiresRerunGating(t *testing.T) {
	r := Rule{Name: "rr", Priority: 1, RequiresRerun: true, Match: func(Command) bool { return true }, Fix: func(Command) []string { return []string{"fresh"} }}
	e := New([]Rule{r})

	if got := e.Correct(Command{Script: "make build"}, false); len(got) != 0 {
		t.Errorf("RequiresRerun rule should be skipped for non-read-only command: %+v", got)
	}
	if got := e.Correct(Command{Script: "ls -l"}, false); len(got) != 1 {
		t.Errorf("RequiresRerun rule should run for read-only command: %+v", got)
	}
	if got := e.Correct(Command{Script: "make build"}, true); len(got) != 1 {
		t.Errorf("--rerun should allow the rule: %+v", got)
	}
}

// TestNeverExecutes is a guard against accidental side effects: the engine must
// be a pure function. We can't prove non-execution structurally here, but we
// assert it returns strings and touches nothing by running a "dangerous"
// script through it and checking it just echoes a corrected string.
func TestPureStringOutput(t *testing.T) {
	got := DefaultEngine().Correct(Command{
		Script:   "rm somedir",
		Stderr:   "rm: somedir: is a directory",
		ExitCode: 1,
	}, false)
	if len(got) == 0 || !strings.Contains(got[0].Command, "rm -r") {
		t.Fatalf("expected an 'rm -r' suggestion, got %+v", got)
	}
}
