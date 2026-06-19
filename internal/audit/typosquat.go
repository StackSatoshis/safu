package audit

import (
	"bufio"
	"embed"
	"strings"
	"sync"
)

//go:embed data/top_pypi.txt data/top_npm.txt data/top_crates.txt data/top_homebrew.txt
var topFS embed.FS

// Typosquat checks a package name against a bundled list of popular packages
// per ecosystem and flags near-misses (edit distance 1–2), the classic
// typosquat window (§5.4). The bundled lists are small launch sets; their
// sourcing and refresh cadence are deferred (SPEC.md §14).
type Typosquat struct {
	once  sync.Once
	files map[Ecosystem]string
	lists map[Ecosystem][]string
}

// TyposquatHit reports a near-miss against a popular package.
type TyposquatHit struct {
	Target   string // the popular package the name resembles
	Distance int    // Levenshtein distance (1 or 2)
}

// DefaultTyposquat returns a checker backed by the embedded top-package lists.
func DefaultTyposquat() *Typosquat {
	return &Typosquat{
		files: map[Ecosystem]string{
			PyPI:     "data/top_pypi.txt",
			NPM:      "data/top_npm.txt",
			Crates:   "data/top_crates.txt",
			Homebrew: "data/top_homebrew.txt",
		},
	}
}

func (t *Typosquat) load() {
	t.lists = make(map[Ecosystem][]string, len(t.files))
	for eco, path := range t.files {
		t.lists[eco] = readList(path)
	}
}

func readList(path string) []string {
	f, err := topFS.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, strings.ToLower(line))
	}
	return out
}

// Check returns a TyposquatHit if name is a near-miss (distance 1–2) of a
// popular package in the same ecosystem, and nil otherwise. An exact match
// (distance 0) is the popular package itself and is never a hit. Very short
// names are skipped to avoid false positives.
func (t *Typosquat) Check(eco Ecosystem, name string) *TyposquatHit {
	t.once.Do(t.load)

	n := strings.ToLower(strings.TrimSpace(name))
	if len(n) < 4 {
		return nil
	}
	best := -1
	bestTarget := ""
	for _, top := range t.lists[eco] {
		if n == top {
			return nil // it *is* the popular package
		}
		// Quick length gate: distance >= |len diff|.
		if abs(len(n)-len(top)) > 2 {
			continue
		}
		d := levenshtein(n, top, 2)
		if d >= 1 && d <= 2 && (best == -1 || d < best) {
			best, bestTarget = d, top
		}
	}
	if best == -1 {
		return nil
	}
	return &TyposquatHit{Target: bestTarget, Distance: best}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// levenshtein computes the edit distance between a and b, short-circuiting once
// the distance is known to exceed max (returns max+1 in that case).
func levenshtein(a, b string, max int) int {
	la, lb := len(a), len(b)
	if abs(la-lb) > max {
		return max + 1
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > max {
			return max + 1
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
