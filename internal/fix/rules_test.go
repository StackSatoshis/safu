package fix

import (
	"strings"
	"testing"
)

// topFix returns the highest-ranked corrected command for a captured Command,
// or "" if none.
func topFix(c Command) string {
	got := DefaultEngine().Correct(c, false)
	if len(got) == 0 {
		return ""
	}
	return got[0].Command
}

func TestRules(t *testing.T) {
	cases := []struct {
		name   string
		cmd    Command
		want   string // expected top correction ("" = expect none)
		substr bool   // match by substring instead of exact
	}{
		{
			name: "sudo on permission denied",
			cmd:  Command{Script: "apt update", Stderr: "E: Could not open lock file - Permission denied", ExitCode: 1},
			want: "sudo apt update",
		},
		{
			name: "sudo not re-suggested when already sudo",
			cmd:  Command{Script: "sudo apt update", Stderr: "permission denied", ExitCode: 1},
			want: "",
		},
		{
			name:   "git did you mean",
			cmd:    Command{Script: "git comit -m hi", Stderr: "git: 'comit' is not a git command. See 'git --help'.\n\nThe most similar command is\n\tcommit", ExitCode: 1},
			want:   "git commit -m hi",
			substr: false,
		},
		{
			name: "git set upstream uses git's own suggestion",
			cmd:  Command{Script: "git push", Stderr: "fatal: The current branch main has no upstream branch.\nTo push the current branch and set the remote as upstream, use\n\n    git push --set-upstream origin main\n", ExitCode: 128},
			want: "git push --set-upstream origin main",
		},
		{
			name: "cd into missing dir",
			cmd:  Command{Script: "cd build/out", Stderr: "cd: no such file or directory: build/out", ExitCode: 1},
			want: "mkdir -p build/out && cd build/out",
		},
		{
			name: "mkdir missing -p",
			cmd:  Command{Script: "mkdir a/b/c", Stderr: "mkdir: cannot create directory 'a/b/c': No such file or directory", ExitCode: 1},
			want: "mkdir -p a/b/c",
		},
		{
			name: "cp directory without -r",
			cmd:  Command{Script: "cp src dst", Stderr: "cp: src is a directory (not copied).", ExitCode: 1},
			want: "cp -r src dst",
		},
		{
			name: "rm directory without -r",
			cmd:  Command{Script: "rm logs", Stderr: "rm: logs: is a directory", ExitCode: 1},
			want: "rm -r logs",
		},
		{
			name: "chmod +x for non-executable script",
			cmd:  Command{Script: "./deploy.sh", Stderr: "bash: ./deploy.sh: Permission denied", ExitCode: 126},
			want: "chmod +x ./deploy.sh && ./deploy.sh",
		},
		{
			name: "man no entry falls back to --help",
			cmd:  Command{Script: "man rg", Stderr: "No manual entry for rg", ExitCode: 16},
			want: "rg --help",
		},
		{
			name: "command typo gti -> git",
			cmd:  Command{Script: "gti status", Stderr: "zsh: command not found: gti", ExitCode: 127},
			want: "git status",
		},
		{
			name: "python -> python3 on not found",
			cmd:  Command{Script: "python app.py", Stderr: "bash: python: command not found", ExitCode: 127},
			want: "python3 app.py",
		},
		{
			name: "no match on clean exit",
			cmd:  Command{Script: "ls", Stderr: "", ExitCode: 0},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := topFix(tc.cmd)
			if tc.substr {
				if !strings.Contains(got, tc.want) {
					t.Errorf("top fix = %q, want substring %q", got, tc.want)
				}
				return
			}
			if got != tc.want {
				t.Errorf("top fix = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGitMultipleSuggestions(t *testing.T) {
	c := Command{
		Script:   "git puhs",
		Stderr:   "git: 'puhs' is not a git command. See 'git --help'.\n\nThe most similar commands are\n\tpush\n\tpull\n",
		ExitCode: 1,
	}
	got := DefaultEngine().Correct(c, false)
	if len(got) < 2 {
		t.Fatalf("expected multiple suggestions, got %+v", got)
	}
	if got[0].Command != "git push" || got[1].Command != "git pull" {
		t.Errorf("suggestions = %+v, want git push then git pull", got)
	}
}
