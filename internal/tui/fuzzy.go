// Package tui is safu's shared built-in interactive layer (SPEC.md §8.4): the
// setup/config wizard and the activity-log browser, plus the small in-house
// fuzzy matcher they share. Built-in, not fzf/skim — so it works identically
// over SSH on a fresh box with no extra tools installed.
//
// This file is the fuzzy matcher: a self-contained scorer (§8.4 explicitly
// calls for a skim-grade algorithm vendored inside the TUI rather than a
// dependency). It is pure and unit-tested.
package tui

import (
	"sort"
	"strings"
	"unicode"
)

// Match is a scored candidate.
type Match struct {
	Index int    // position in the input slice
	Score int    // higher is a better match
	Str   string // the candidate
}

// scoring weights
const (
	bonusBoundary    = 10 // match at start or just after a separator
	bonusCamel       = 8  // match at a lower→upper camelCase boundary
	bonusConsecutive = 5  // match immediately following another match
	scoreBase        = 1  // every matched char
)

// Score returns a match score for query against candidate and whether the
// query matches at all (case-insensitive subsequence). An empty query matches
// everything with score 0.
func Score(query, candidate string) (int, bool) {
	if query == "" {
		return 0, true
	}
	q := []rune(strings.ToLower(query))
	c := []rune(candidate)
	lc := []rune(strings.ToLower(candidate))

	qi, total := 0, 0
	prevMatched := false
	for ci := 0; ci < len(c) && qi < len(q); ci++ {
		if lc[ci] != q[qi] {
			prevMatched = false
			continue
		}
		s := scoreBase
		switch {
		case ci == 0 || isSeparator(c[ci-1]):
			s += bonusBoundary
		case unicode.IsLower(c[ci-1]) && unicode.IsUpper(c[ci]):
			s += bonusCamel
		}
		if prevMatched {
			s += bonusConsecutive
		}
		total += s
		prevMatched = true
		qi++
	}
	if qi != len(q) {
		return 0, false
	}
	return total, true
}

// Rank scores all candidates and returns the matching ones, best first. Ties
// break toward shorter candidates, then original order (stable).
func Rank(query string, candidates []string) []Match {
	var ms []Match
	for i, c := range candidates {
		if s, ok := Score(query, c); ok {
			ms = append(ms, Match{Index: i, Score: s, Str: c})
		}
	}
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].Score != ms[j].Score {
			return ms[i].Score > ms[j].Score
		}
		if len(ms[i].Str) != len(ms[j].Str) {
			return len(ms[i].Str) < len(ms[j].Str)
		}
		return ms[i].Index < ms[j].Index
	})
	return ms
}

func isSeparator(r rune) bool {
	switch r {
	case '/', '\\', '_', '-', '.', ' ', ':', '@':
		return true
	}
	return false
}
