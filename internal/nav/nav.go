// Package nav implements safu's smart-navigation frecency database (SPEC.md
// §7): a recency-weighted record of visited directories used to power
// `safu z <query>`. It is local-only and NEVER networked (invariant, §7.3);
// it honors an exclude list and is wipeable on demand.
package nav

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// agingThreshold is the total-rank ceiling past which all ranks are aged down,
// keeping the database bounded (mirrors zoxide's behavior).
const agingThreshold = 9000.0

// Entry is a tracked directory with its frecency inputs.
type Entry struct {
	Path       string  `json:"path"`
	Rank       float64 `json:"rank"`
	LastAccess int64   `json:"last_access"` // unix seconds
}

// DB is the on-disk frecency store.
type DB struct {
	path       string
	maxEntries int
	now        func() time.Time
	entries    map[string]*Entry
}

// Open loads (or initializes) the database under dataDir.
func Open(dataDir string, maxEntries int) (*DB, error) {
	if maxEntries <= 0 {
		maxEntries = 10000
	}
	d := &DB{
		path:       filepath.Join(dataDir, "db"),
		maxEntries: maxEntries,
		now:        time.Now,
		entries:    map[string]*Entry{},
	}
	if err := d.load(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *DB) load() error {
	f, err := os.Open(d.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open nav db: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // skip corrupt lines
		}
		if e.Path != "" {
			ent := e
			d.entries[e.Path] = &ent
		}
	}
	return sc.Err()
}

// Add records a visit to path: bumps its rank and access time, ages/evicts as
// needed, and persists. Non-absolute or empty paths are ignored.
func (d *DB) Add(path string) error {
	path = filepath.Clean(path)
	if path == "" || !filepath.IsAbs(path) {
		return nil
	}
	now := d.now().Unix()
	e, ok := d.entries[path]
	if !ok {
		e = &Entry{Path: path}
		d.entries[path] = e
	}
	e.Rank++
	e.LastAccess = now

	d.age()
	d.evict()
	return d.save()
}

// age scales all ranks down once the total exceeds the threshold, dropping
// entries that fall below 1.
func (d *DB) age() {
	var total float64
	for _, e := range d.entries {
		total += e.Rank
	}
	if total <= agingThreshold {
		return
	}
	for path, e := range d.entries {
		e.Rank *= 0.99
		if e.Rank < 1 {
			delete(d.entries, path)
		}
	}
}

// evict drops the lowest-frecency entries when over the cap.
func (d *DB) evict() {
	if len(d.entries) <= d.maxEntries {
		return
	}
	list := d.sorted()
	for _, e := range list[d.maxEntries:] {
		delete(d.entries, e.Path)
	}
}

// frecency scores an entry: higher rank and more-recent access score higher.
func frecency(e Entry, now time.Time) float64 {
	dur := now.Unix() - e.LastAccess
	switch {
	case dur < 3600:
		return e.Rank * 4
	case dur < 86400:
		return e.Rank * 2
	case dur < 604800:
		return e.Rank * 0.5
	default:
		return e.Rank * 0.25
	}
}

// sorted returns entries by descending frecency.
func (d *DB) sorted() []Entry {
	now := d.now()
	out := make([]Entry, 0, len(d.entries))
	for _, e := range d.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		return frecency(out[i], now) > frecency(out[j], now)
	})
	return out
}

// Query returns the highest-frecency directory matching the query and still
// present on disk. A query that is itself an existing directory resolves to its
// cleaned absolute path (literal fallback, §7.1). Dead paths encountered are
// pruned.
func (d *DB) Query(query string) (string, bool) {
	if lit, ok := literalDir(query); ok {
		return lit, true
	}
	tokens := tokenize(query)
	now := d.now()

	var best string
	bestScore := -1.0
	var dead []string
	for path, e := range d.entries {
		if !matches(path, tokens) {
			continue
		}
		if !dirExists(path) {
			dead = append(dead, path)
			continue
		}
		if s := frecency(*e, now); s > bestScore {
			best, bestScore = path, s
		}
	}
	if len(dead) > 0 {
		for _, p := range dead {
			delete(d.entries, p)
		}
		_ = d.save()
	}
	return best, best != ""
}

// List returns all entries by descending frecency.
func (d *DB) List() []Entry { return d.sorted() }

// Score exposes an entry's current frecency (for display).
func (d *DB) Score(e Entry) float64 { return frecency(e, d.now()) }

// Clear wipes the database.
func (d *DB) Clear() error {
	d.entries = map[string]*Entry{}
	if err := os.Remove(d.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear nav db: %w", err)
	}
	return nil
}

func (d *DB) save() error {
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return fmt.Errorf("create nav dir: %w", err)
	}
	tmp := d.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open nav temp: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, e := range d.entries {
		line, err := json.Marshal(e)
		if err != nil {
			f.Close()
			return err
		}
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
	return os.Rename(tmp, d.path)
}

// --- matching & helpers ---

func tokenize(query string) []string {
	return strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return r == ' ' || r == '/' || r == os.PathSeparator
	})
}

// matches reports whether the lowercased path contains the tokens in order
// (subsequence of substrings) and the final token appears in the basename.
func matches(path string, tokens []string) bool {
	if len(tokens) == 0 {
		return true
	}
	lp := strings.ToLower(path)
	idx := 0
	for _, tok := range tokens {
		j := strings.Index(lp[idx:], tok)
		if j < 0 {
			return false
		}
		idx += j + len(tok)
	}
	base := strings.ToLower(filepath.Base(path))
	return strings.Contains(base, tokens[len(tokens)-1])
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// literalDir returns the cleaned absolute path if query names an existing
// directory (the plain-cd fallback).
func literalDir(query string) (string, bool) {
	if query == "" {
		return "", false
	}
	if fi, err := os.Stat(query); err == nil && fi.IsDir() {
		if abs, err := filepath.Abs(query); err == nil {
			return abs, true
		}
	}
	return "", false
}

// IsExcluded reports whether path matches any exclude glob (after expanding a
// leading ~ and environment variables). §7.3.
func IsExcluded(path string, patterns []string) bool {
	path = filepath.Clean(path)
	for _, pat := range patterns {
		p := expand(pat)
		if p == "" {
			continue
		}
		if filepath.Clean(p) == path {
			return true
		}
		if ok, _ := filepath.Match(p, path); ok {
			return true
		}
	}
	return false
}

func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = home + p[1:]
		}
	}
	return os.ExpandEnv(p)
}
