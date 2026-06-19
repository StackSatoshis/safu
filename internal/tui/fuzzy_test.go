package tui

import "testing"

func TestScoreMatch(t *testing.T) {
	cases := []struct {
		query, cand string
		want        bool
	}{
		{"", "anything", true},
		{"gc", "git-commit", true},
		{"GC", "git-commit", true}, // case-insensitive
		{"abc", "a_b_c", true},
		{"abc", "axc", false}, // missing b
		{"xyz", "git", false},
		{"safu", "safu-web", true},
	}
	for _, c := range cases {
		if _, ok := Score(c.query, c.cand); ok != c.want {
			t.Errorf("Score(%q,%q) matched=%v, want %v", c.query, c.cand, ok, c.want)
		}
	}
}

func TestScoreBonuses(t *testing.T) {
	// Boundary match should outscore a mid-word match of the same query.
	boundary, _ := Score("bar", "foo-bar")
	mid, _ := Score("bar", "foobart")
	if boundary <= mid {
		t.Errorf("boundary score %d should exceed mid-word %d", boundary, mid)
	}
	// Consecutive run should outscore a scattered match.
	consec, _ := Score("abc", "abcde")
	scattered, _ := Score("abc", "axbxcx")
	if consec <= scattered {
		t.Errorf("consecutive %d should exceed scattered %d", consec, scattered)
	}
}

func TestRankOrder(t *testing.T) {
	got := Rank("src", []string{"resource", "src", "my-src-dir", "scratch"})
	if len(got) == 0 {
		t.Fatal("expected matches")
	}
	// "src" (boundary at 0, consecutive, short) should rank first.
	if got[0].Str != "src" {
		t.Errorf("top match = %q, want %q (order: %+v)", got[0].Str, "src", got)
	}
	// "scratch" matches s..r..c? s,c,r — "src": s then r? s-c-r-a-t-c-h: s(0), need r -> at idx2 'r'? order s,r,c: s@0, r@2, c@5 -> matches. It's a worse match; just ensure src first.
}

func TestRankFiltersNonMatches(t *testing.T) {
	got := Rank("zzz", []string{"abc", "def"})
	if len(got) != 0 {
		t.Errorf("expected no matches, got %+v", got)
	}
}
