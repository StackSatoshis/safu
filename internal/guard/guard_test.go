package guard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testEnv builds an Env rooted in temp dirs: a home, and a cwd nested two deep
// so "above cwd" rules have a parent to target.
func testEnv(t *testing.T) Env {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	cwd := filepath.Join(base, "work", "proj")
	for _, d := range []string{home, cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Env{
		Home:       home,
		Cwd:        cwd,
		MountRoots: []string{"/"},
		Now:        func() time.Time { return time.Unix(1_700_000_000, 0) },
	}
}

func hasFinding(fs []Finding, rule string) bool {
	for _, f := range fs {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func TestParseRmFlags(t *testing.T) {
	env := testEnv(t)
	c := Parse("rm", []string{"-rf", "a", "b"}, env)
	if !c.Recursive || !c.Force {
		t.Errorf("combined -rf not parsed: recursive=%v force=%v", c.Recursive, c.Force)
	}
	if len(c.Targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(c.Targets))
	}
	if c.Targets[0].Abs != filepath.Join(env.Cwd, "a") {
		t.Errorf("target not resolved against cwd: %q", c.Targets[0].Abs)
	}
}

func TestParseDDTarget(t *testing.T) {
	c := Parse("dd", []string{"if=/dev/zero", "of=/dev/sda", "bs=1m"}, testEnv(t))
	if c.Options["of"] != "/dev/sda" {
		t.Errorf("dd of= not captured: %v", c.Options)
	}
}

func TestClassify(t *testing.T) {
	env := testEnv(t)
	parent := filepath.Dir(env.Cwd)
	outside := filepath.Join(filepath.Dir(parent), "elsewhere")
	inside := filepath.Join(env.Cwd, "build")

	// A git repo root inside cwd.
	gitRepo := filepath.Join(env.Cwd, "repo")
	if err := os.MkdirAll(filepath.Join(gitRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		cmd      string
		argv     []string
		level    string
		wantRisk Risk
		wantRule string // must be present (empty = don't check)
	}{
		{"off is no-op", "rm", []string{"-rf", "/"}, "off", Safe, ""},
		{"rm -rf root blocks", "rm", []string{"-rf", "/"}, "light", Block, "rm-root"},
		{"rm -rf home blocks", "rm", []string{"-rf", env.Home}, "light", Block, "rm-home"},
		{"rm -rf parent-of-cwd blocks", "rm", []string{"-rf", parent}, "light", Block, "rm-above-cwd"},
		{"rm -rf outside cwd warns at standard", "rm", []string{"-rf", outside}, "standard", Warn, "rm-outside-cwd"},
		{"rm -rf outside cwd safe at light", "rm", []string{"-rf", outside}, "light", Safe, ""},
		{"rm -rf inside cwd is safe", "rm", []string{"-rf", inside}, "standard", Safe, ""},
		{"rm -rf git repo root warns", "rm", []string{"-rf", gitRepo}, "standard", Warn, "rm-git-root"},
		{"rm file non-recursive inside is safe", "rm", []string{"x"}, "standard", Safe, ""},
		{"rm file outside warns at paranoid", "rm", []string{outside}, "paranoid", Warn, "rm-outside-cwd-paranoid"},
		{"dd to device blocks", "dd", []string{"if=/dev/zero", "of=/dev/sda"}, "light", Block, "dd-device"},
		{"mkfs device blocks", "mkfs", []string{"/dev/sdb"}, "light", Block, "mkfs-device"},
		{"chmod -R 777 outside warns", "chmod", []string{"-R", "777", outside}, "standard", Warn, "chmod-r-world"},
		{"chmod -R 777 inside is safe", "chmod", []string{"-R", "777", inside}, "standard", Safe, ""},
		{"chmod system path warns", "chmod", []string{"644", "/etc/hosts"}, "standard", Warn, "chmod-system"},
		{"chown -R system warns", "chown", []string{"-R", "root", "/usr/local"}, "standard", Warn, "chown-system"},
		{"git force-push warns", "git", []string{"push", "--force", "origin", "main"}, "standard", Warn, "git-force-push"},
		{"git force-with-lease is safe", "git", []string{"push", "--force-with-lease"}, "standard", Safe, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := Parse(tc.cmd, tc.argv, env)
			d := Classify(cmd, tc.level, env)
			if d.Risk != tc.wantRisk {
				t.Errorf("Risk = %q, want %q (findings: %+v)", d.Risk, tc.wantRisk, d.Findings)
			}
			if tc.wantRule != "" && !hasFinding(d.Findings, tc.wantRule) {
				t.Errorf("missing rule %q (findings: %+v)", tc.wantRule, d.Findings)
			}
		})
	}
}

func TestBuildPreview(t *testing.T) {
	env := testEnv(t)
	dir := filepath.Join(env.Cwd, "tree")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("worldworld"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := Parse("rm", []string{"-rf", dir}, env)
	p := BuildPreview(cmd, env)
	if p.Files != 2 {
		t.Errorf("Files = %d, want 2", p.Files)
	}
	if p.Dirs != 2 { // tree + tree/sub
		t.Errorf("Dirs = %d, want 2", p.Dirs)
	}
	if p.Bytes != 15 {
		t.Errorf("Bytes = %d, want 15", p.Bytes)
	}
}

func TestPreviewWarnings(t *testing.T) {
	env := testEnv(t)
	cmd := Parse("rm", []string{"-rf", env.Home}, env)
	p := BuildPreview(cmd, env)
	found := false
	for _, w := range p.Warnings {
		if w == "target resolves to your home directory" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected home-directory warning, got %v", p.Warnings)
	}
}

type fakePrompter struct{ answer bool }

func (f fakePrompter) Confirm(string) bool { return f.answer }

func TestDecide(t *testing.T) {
	warn := Decision{Risk: Warn}
	block := Decision{Risk: Block}
	safe := Decision{Risk: Safe}

	cases := []struct {
		name string
		dec  Decision
		opt  Options
		ans  bool
		want Action
	}{
		{"safe approves", safe, Options{}, false, Approve},
		{"warn + yes approves", warn, Options{Yes: true}, false, Approve},
		{"warn interactive yes approves", warn, Options{Interactive: true}, true, Approve},
		{"warn interactive no blocks", warn, Options{Interactive: true}, false, BlockIt},
		{"warn non-interactive blocks", warn, Options{Interactive: false}, true, BlockIt},
		{"block without force blocks", block, Options{}, true, BlockIt},
		{"block with force approves", block, Options{Force: true}, false, Approve},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.dec, tc.opt, fakePrompter{tc.ans}); got != tc.want {
				t.Errorf("Decide = %v, want %v", got, tc.want)
			}
		})
	}
}
