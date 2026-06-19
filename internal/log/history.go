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

// HistoryEntry is one recorded shell command (SPEC.md §8.2). It lives in a
// separate JSONL file from the activity log and is opt-in. Local-only, never
// networked (§8.3).
type HistoryEntry struct {
	Time    time.Time `json:"time"`
	Command string    `json:"command"`
	Dir     string    `json:"dir,omitempty"`
	Exit    int       `json:"exit"`
}

// History is the general shell-history store.
type History struct {
	path          string
	retentionDays int
	now           func() time.Time
}

// NewHistory returns a History writing to <dir>/history.jsonl.
func NewHistory(dir string, retentionDays int) *History {
	return &History{
		path:          filepath.Join(dir, "history.jsonl"),
		retentionDays: retentionDays,
		now:           time.Now,
	}
}

// Path returns the history file path.
func (h *History) Path() string { return h.path }

// Append records one command (raw; de-duplication happens only on display).
func (h *History) Append(e HistoryEntry) error {
	if e.Time.IsZero() {
		e.Time = h.now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open history: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write history: %w", err)
	}
	return nil
}

// Read returns history entries oldest-first. If grep is non-empty, only
// commands containing it (case-insensitive) are returned.
func (h *History) Read(grep string) ([]HistoryEntry, error) {
	f, err := os.Open(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open history: %w", err)
	}
	defer f.Close()

	low := strings.ToLower(grep)
	var out []HistoryEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e HistoryEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if low != "" && !strings.Contains(strings.ToLower(e.Command), low) {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// Clear removes the history file.
func (h *History) Clear() error {
	if err := os.Remove(h.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear history: %w", err)
	}
	return nil
}

// Trim drops entries older than the retention window (cheap unless the oldest
// line is stale). Mirrors Logger.Trim.
func (h *History) Trim() error {
	if h.retentionDays <= 0 {
		return nil
	}
	cutoff := h.now().AddDate(0, 0, -h.retentionDays)
	f, err := os.Open(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open history: %w", err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var kept [][]byte
	stale := false
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		if len(line) == 0 {
			continue
		}
		var e HistoryEntry
		if err := json.Unmarshal(line, &e); err == nil && e.Time.Before(cutoff) {
			stale = true
			continue
		}
		kept = append(kept, line)
	}
	f.Close()
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan history: %w", err)
	}
	if !stale {
		return nil
	}
	return rewriteLines(h.path, kept)
}

// DedupLatest removes duplicate commands, keeping each command's most-recent
// occurrence and preserving order. Input is oldest-first; output is oldest-
// first (§8.2: de-duplicated on display, raw log preserved).
func DedupLatest(entries []HistoryEntry) []HistoryEntry {
	lastIdx := make(map[string]int, len(entries))
	for i, e := range entries {
		lastIdx[e.Command] = i
	}
	out := make([]HistoryEntry, 0, len(entries))
	for i, e := range entries {
		if lastIdx[e.Command] == i {
			out = append(out, e)
		}
	}
	return out
}

// HistoryExcluded reports whether a command matches any exclude pattern, so it
// must never be recorded (§8.2 secret filter). Patterns are simple globs where
// a surrounding "*" means "contains"; matching is case-insensitive.
func HistoryExcluded(command string, patterns []string) bool {
	cmd := strings.ToLower(command)
	for _, pat := range patterns {
		inner := strings.ToLower(strings.Trim(pat, "*"))
		if inner == "" {
			continue
		}
		if strings.Contains(cmd, inner) {
			return true
		}
	}
	return false
}

// rewriteLines atomically replaces a JSONL file's contents.
func rewriteLines(path string, lines [][]byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open temp: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, line := range lines {
		w.Write(line)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
