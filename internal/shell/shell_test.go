package shell

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden snippet files")

func TestCommands(t *testing.T) {
	got := Commands([]string{"rm", "git-push-force", "dd", "mkfs", "chmod-r", "chown-r", "safu", "rm"})
	want := []string{"chmod", "chown", "dd", "git", "mkfs", "rm"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Commands = %v, want %v", got, want)
	}
}

func TestParse(t *testing.T) {
	for _, s := range []string{"bash", "ZSH", " fish "} {
		if _, err := Parse(s); err != nil {
			t.Errorf("Parse(%q) errored: %v", s, err)
		}
	}
	if _, err := Parse("powershell"); err == nil {
		t.Error("Parse(powershell) should error")
	}
}

func TestSnippetGolden(t *testing.T) {
	wrapped := []string{"rm", "git-push-force", "dd"}
	for _, sh := range []Shell{Bash, Zsh, Fish} {
		got, err := Snippet(sh, wrapped)
		if err != nil {
			t.Fatalf("Snippet(%s): %v", sh, err)
		}
		golden := filepath.Join("testdata", string(sh)+".golden")
		if *update {
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden %s: %v (run with -update)", golden, err)
		}
		if got != string(want) {
			t.Errorf("%s snippet mismatch with %s:\n--- got ---\n%s", sh, golden, got)
		}
	}
}

func TestNavSnippetGolden(t *testing.T) {
	for _, sh := range []Shell{Bash, Zsh, Fish} {
		got, err := NavSnippet(sh, "z")
		if err != nil {
			t.Fatalf("NavSnippet(%s): %v", sh, err)
		}
		golden := filepath.Join("testdata", "nav-"+string(sh)+".golden")
		if *update {
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden %s: %v (run with -update)", golden, err)
		}
		if got != string(want) {
			t.Errorf("%s nav snippet mismatch:\n--- got ---\n%s", sh, got)
		}
	}
}

func TestFixSnippetGolden(t *testing.T) {
	for _, sh := range []Shell{Bash, Zsh} {
		got, err := FixSnippet(sh, []string{"fix", "wtf"})
		if err != nil {
			t.Fatalf("FixSnippet(%s): %v", sh, err)
		}
		golden := filepath.Join("testdata", "fix-"+string(sh)+".golden")
		if *update {
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden %s: %v (run with -update)", golden, err)
		}
		if got != string(want) {
			t.Errorf("%s fix snippet mismatch:\n--- got ---\n%s", sh, got)
		}
	}
}

func TestFixSnippetFishUnsupported(t *testing.T) {
	if _, err := FixSnippet(Fish, nil); err == nil {
		t.Error("fish fix integration should report unsupported")
	}
}

func TestNavSnippetCustomCmd(t *testing.T) {
	got, err := NavSnippet(Zsh, "j")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "j() {") {
		t.Errorf("custom jump command name not applied:\n%s", got)
	}
}

// TestSnippetFailOpen asserts the safety-critical properties of the generated
// snippet regardless of exact formatting.
func TestSnippetFailOpen(t *testing.T) {
	got, err := Snippet(Bash, []string{"rm"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`safu guard --as rm -- "$@"`, // delegates to guard
		`command rm "$@"`,            // falls through to the real command
		`-z "$SAFU_DISABLE"`,         // honors the kill switch
		`command -v safu`,            // fail-open if safu is missing
	} {
		if !strings.Contains(got, want) {
			t.Errorf("snippet missing %q:\n%s", want, got)
		}
	}
}

func TestInstallToRC(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".bashrc")
	if err := os.WriteFile(rc, []byte("export PATH=/usr/bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backup := rc + ".safu-backup-test"

	changed, err := InstallToRC(rc, "rm() { :; }", backup)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true on first install")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("backup not created: %v", err)
	}
	content, _ := os.ReadFile(rc)
	if !IsInstalled(string(content)) {
		t.Error("rc not marked installed")
	}
	if !strings.Contains(string(content), "export PATH=/usr/bin") {
		t.Error("original rc content lost")
	}

	// Idempotent: second install is a no-op.
	changed, err = InstallToRC(rc, "rm() { :; }", backup+"2")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected changed=false on second install")
	}
	if strings.Count(string(mustRead(t, rc)), markerStart) != 1 {
		t.Error("duplicate safu block appended")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
