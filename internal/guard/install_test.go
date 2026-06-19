package guard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/StackSatoshis/safu/internal/audit"
)

func pkgKey(p audit.Package) string {
	return string(p.Ecosystem) + ":" + p.Name + "@" + p.Version
}

func TestParseInstall(t *testing.T) {
	cases := []struct {
		name     string
		cmd      string
		argv     []string
		wantIs   bool
		wantEco  audit.Ecosystem
		wantPkgs []string // ecosystem:name@version
	}{
		{"pip install pinned + unpinned", "pip", []string{"install", "requests==2.31.0", "flask"}, true, audit.PyPI,
			[]string{"pypi:requests@2.31.0", "pypi:flask@"}},
		{"pip extras and specifier stripped", "pip3", []string{"install", "uvicorn[standard]>=0.20"}, true, audit.PyPI,
			[]string{"pypi:uvicorn@"}},
		{"pip skips flags and urls", "pip", []string{"install", "-U", "--index-url", "https://x/y", "numpy", "git+https://h/r.git"}, true, audit.PyPI,
			[]string{"pypi:numpy@"}},
		{"uv pip install", "uv", []string{"pip", "install", "ruff==0.1.0"}, true, audit.PyPI,
			[]string{"pypi:ruff@0.1.0"}},
		{"pip non-install", "pip", []string{"list"}, false, "", nil},
		{"npm add scoped + versioned", "npm", []string{"install", "@scope/pkg@1.2.3", "lodash@4.17.21", "express"}, true, audit.NPM,
			[]string{"npm:@scope/pkg@1.2.3", "npm:lodash@4.17.21", "npm:express@"}},
		{"npm skips github shorthand", "npm", []string{"i", "user/repo", "chalk"}, true, audit.NPM,
			[]string{"npm:chalk@"}},
		{"yarn add", "yarn", []string{"add", "react@18.2.0"}, true, audit.NPM, []string{"npm:react@18.2.0"}},
		{"cargo add versioned", "cargo", []string{"add", "serde@1.0", "anyhow"}, true, audit.Crates,
			[]string{"crates:serde@1.0", "crates:anyhow@"}},
		{"cargo install with --version", "cargo", []string{"install", "ripgrep", "--version", "14.0.0"}, true, audit.Crates,
			[]string{"crates:ripgrep@14.0.0"}},
		{"brew install", "brew", []string{"install", "wget", "node@18"}, true, audit.Homebrew,
			[]string{"homebrew:wget@", "homebrew:node@18@"}},
		{"unknown command", "ls", []string{"-la"}, false, "", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eco, pkgs, is := ParseInstall(tc.cmd, tc.argv, "/tmp")
			if is != tc.wantIs {
				t.Fatalf("isInstall = %v, want %v", is, tc.wantIs)
			}
			if !is {
				return
			}
			if eco != tc.wantEco {
				t.Errorf("ecosystem = %q, want %q", eco, tc.wantEco)
			}
			var got []string
			for _, p := range pkgs {
				got = append(got, pkgKey(p))
			}
			if len(got) != len(tc.wantPkgs) {
				t.Fatalf("packages = %v, want %v", got, tc.wantPkgs)
			}
			for i := range got {
				if got[i] != tc.wantPkgs[i] {
					t.Errorf("package[%d] = %q, want %q", i, got[i], tc.wantPkgs[i])
				}
			}
		})
	}
}

// Note: "node@18@" above reflects that brew keeps the whole "node@18" as the
// package name (versioned formula), so the key's version segment is empty.

func TestParseRequirementsFile(t *testing.T) {
	dir := t.TempDir()
	req := filepath.Join(dir, "requirements.txt")
	content := `# a comment
requests==2.31.0
flask>=2.0   # inline comment
-e .
git+https://github.com/x/y.git
django ; python_version >= "3.8"
`
	if err := os.WriteFile(req, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, pkgs, is := ParseInstall("pip", []string{"install", "-r", "requirements.txt"}, dir)
	if !is {
		t.Fatal("expected install command")
	}
	var got []string
	for _, p := range pkgs {
		got = append(got, pkgKey(p))
	}
	want := []string{"pypi:requests@2.31.0", "pypi:flask@", "pypi:django@"}
	if len(got) != len(want) {
		t.Fatalf("requirements packages = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("req[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSummarizeAudit(t *testing.T) {
	mk := func(level audit.Level, blocked bool) audit.Verdict {
		return audit.Verdict{Level: level, Blocked: blocked}
	}
	cases := []struct {
		name     string
		verdicts []audit.Verdict
		wantRisk Risk
		wantMal  bool
	}{
		{"all safe", []audit.Verdict{mk(audit.Safe, false), mk(audit.Safe, false)}, Safe, false},
		{"caution prompts", []audit.Verdict{mk(audit.Caution, false)}, Warn, false},
		{"danger prompts", []audit.Verdict{mk(audit.Danger, false)}, Warn, false},
		{"malicious flagged", []audit.Verdict{mk(audit.Danger, true)}, Warn, true},
		{"mixed", []audit.Verdict{mk(audit.Safe, false), mk(audit.Caution, false), mk(audit.Danger, true)}, Warn, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			risk, mal := SummarizeAudit(tc.verdicts)
			if risk != tc.wantRisk || mal != tc.wantMal {
				t.Errorf("SummarizeAudit = (%v,%v), want (%v,%v)", risk, mal, tc.wantRisk, tc.wantMal)
			}
		})
	}
}
