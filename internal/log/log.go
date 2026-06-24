// Package log implements safu's local-only, plain-text activity log (SPEC.md
// §8.1). It is newline-delimited JSON (JSONL) under ~/.safu/log: one
// human-readable line per event so the file itself is the auditable proof of
// what safu did. It is NEVER networked and never read by the auditor or update
// check (invariant #2).
package log

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Event kinds.
const (
	KindBlock       = "block"        // a destructive command was refused
	KindWarnProceed = "warn_proceed" // user confirmed a warned command
	KindSoftDelete  = "soft_delete"  // targets moved to trash
	KindUndo        = "undo"         // a trashed operation was restored
	KindAudit       = "audit"        // a package-audit verdict
)

// Event is a single activity-log record.
type Event struct {
	Time    time.Time      `json:"time"`
	Kind    string         `json:"kind"`
	Command string         `json:"command,omitempty"`
	Targets []string       `json:"targets,omitempty"`
	Risk    string         `json:"risk,omitempty"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// Logger appends events to a JSONL file and enforces a retention window.
type Logger struct {
	path          string
	retentionDays int
	now           func() time.Time
}

// New returns a Logger writing to <dir>/activity.jsonl. retentionDays <= 0
// disables retention trimming.
func New(dir string, retentionDays int) *Logger {
	return &Logger{
		path:          filepath.Join(dir, "activity.jsonl"),
		retentionDays: retentionDays,
		now:           time.Now,
	}
}

// Path returns the log file path.
func (l *Logger) Path() string { return l.path }

// Append writes one event as a JSON line, creating the directory if needed. If
// e.Time is zero it is stamped with the current time.
func (l *Logger) Append(e Event) error {
	if e.Time.IsZero() {
		e.Time = l.now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write log: %w", err)
	}
	return nil
}

// Filter selects events for Read.
type Filter struct {
	Grep  string    // substring match on the raw JSON line
	Since time.Time // only events at or after this time
	Kinds []string  // if non-empty, only these kinds
}

// Read returns the events matching f, oldest first. A missing log is not an
// error (returns nil).
func (l *Logger) Read(f Filter) ([]Event, error) {
	file, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open log: %w", err)
	}
	defer file.Close()

	var out []Event
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if f.Grep != "" && !strings.Contains(string(line), f.Grep) {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			continue // skip corrupt lines rather than failing the whole read
		}
		if !f.Since.IsZero() && e.Time.Before(f.Since) {
			continue
		}
		if len(f.Kinds) > 0 && !contains(f.Kinds, e.Kind) {
			continue
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("scan log: %w", err)
	}
	return out, nil
}

// Clear removes the log file entirely.
func (l *Logger) Clear() error {
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear log: %w", err)
	}
	return nil
}

// Trim removes events older than the retention window. It is cheap in the
// common case: it only rewrites the file when the oldest (first) line is
// actually stale. Safe to call on every invocation (the on-invocation sweep,
// §8.1 — no daemon).
func (l *Logger) Trim() error {
	if l.retentionDays <= 0 {
		return nil
	}
	cutoff := l.now().AddDate(0, 0, -l.retentionDays)

	file, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open log: %w", err)
	}
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Cheap path: entries are appended chronologically, so if the oldest
	// (first) line isn't stale, nothing needs trimming.
	first := firstNonEmpty(sc)
	if first == nil || !entryStale(first, cutoff) {
		file.Close()
		return nil
	}
	// The first line is stale: rebuild keeping only non-stale lines.
	var kept [][]byte
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		if len(line) == 0 || entryStale(line, cutoff) {
			continue
		}
		kept = append(kept, line)
	}
	file.Close()
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan log: %w", err)
	}
	return l.rewrite(kept)
}

func (l *Logger) rewrite(lines [][]byte) error {
	tmp := l.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open temp log: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, line := range lines {
		w.Write(line)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return fmt.Errorf("write temp log: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return fmt.Errorf("replace log: %w", err)
	}
	return nil
}

// firstNonEmpty advances the scanner to the first non-empty line and returns a
// copy of it (nil if the file has no content).
func firstNonEmpty(sc *bufio.Scanner) []byte {
	for sc.Scan() {
		if b := sc.Bytes(); len(b) > 0 {
			return append([]byte(nil), b...)
		}
	}
	return nil
}

// entryStale reports whether a JSONL line's "time" field is before cutoff. It
// works for both Event and HistoryEntry lines (both carry "time"). Unparseable
// lines are treated as not stale (kept).
func entryStale(line []byte, cutoff time.Time) bool {
	var e struct {
		Time time.Time `json:"time"`
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return false
	}
	return !e.Time.IsZero() && e.Time.Before(cutoff)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
