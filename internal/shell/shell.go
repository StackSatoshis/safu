// Package shell generates the sourced shell-integration snippet for bash, zsh,
// and fish (SPEC.md §3.2). The generated functions wrap the user's enabled
// commands, delegate the decision to `safu guard`, and — critically — fall
// through to the real command if safu is disabled, missing, or errors
// (fail-open, invariant #3).
//
// The `safu guard` subcommand itself lands in a later slice; this package only
// generates the text that will call it. The exit-code contract below is the
// interface guard must honor.
package shell

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

// Guard exit-code contract (consumed by the generated snippet):
//
//	0            -> approved: run the real command
//	ExitHandled  -> safu already performed the action (e.g. soft-delete): do
//	                NOT run the real command; the wrapper returns success
//	ExitBlock    -> blocked: abort, do not run the real command
//	any other    -> safu errored: fail open, run the real command anyway
const (
	ExitBlock   = 10
	ExitHandled = 11
)

// Shell identifies a supported shell.
type Shell string

const (
	Bash Shell = "bash"
	Zsh  Shell = "zsh"
	Fish Shell = "fish"
)

// Parse validates a shell name.
func Parse(s string) (Shell, error) {
	switch Shell(strings.ToLower(strings.TrimSpace(s))) {
	case Bash:
		return Bash, nil
	case Zsh:
		return Zsh, nil
	case Fish:
		return Fish, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (want bash|zsh|fish)", s)
	}
}

// ruleToCommand maps a guard rule id from config (guard.wrapped) to the actual
// shell command that must be wrapped. Several rules collapse onto one command
// (e.g. git-push-force -> git); the guard classifies the specifics.
var ruleToCommand = map[string]string{
	"rm":             "rm",
	"dd":             "dd",
	"mkfs":           "mkfs",
	"git-push-force": "git",
	"chmod-r":        "chmod",
	"chown-r":        "chown",
}

// neverWrap lists shell-critical commands safu must never shadow (SPEC.md §3.3).
var neverWrap = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true,
	"env": true, "exec": true, "command": true, "safu": true,
}

// Commands resolves the config rule ids into the de-duplicated, sorted list of
// real shell commands to wrap, dropping any shell-critical names.
func Commands(wrapped []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, rule := range wrapped {
		cmd, ok := ruleToCommand[rule]
		if !ok {
			cmd = rule // unknown rule: wrap the literal name
		}
		if neverWrap[cmd] || seen[cmd] {
			continue
		}
		seen[cmd] = true
		out = append(out, cmd)
	}
	sort.Strings(out)
	return out
}

type tmplData struct {
	Commands    []string
	ExitBlock   int
	ExitHandled int
}

// Snippet generates the full sourced snippet for sh wrapping the given config
// rule ids.
func Snippet(sh Shell, wrapped []string) (string, error) {
	t, ok := templates[sh]
	if !ok {
		return "", fmt.Errorf("unsupported shell %q", sh)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, tmplData{Commands: Commands(wrapped), ExitBlock: ExitBlock, ExitHandled: ExitHandled}); err != nil {
		return "", fmt.Errorf("generate %s snippet: %w", sh, err)
	}
	return buf.String(), nil
}

var templates = map[Shell]*template.Template{
	Bash: template.Must(template.New("bash").Parse(posixTemplate)),
	Zsh:  template.Must(template.New("zsh").Parse(posixTemplate)),
	Fish: template.Must(template.New("fish").Parse(fishTemplate)),
}

// navData drives the navigation hook templates.
type navData struct {
	Cmd string // the jump command name (default "z")
}

// NavSnippet generates the smart-navigation hook for sh: a chpwd-style recorder
// that calls `safu z --add`, plus the jump function (named cmd) that cd's to
// the resolved directory. The shell performs the cd; safu only resolves
// (a subprocess can't change its parent's cwd).
func NavSnippet(sh Shell, cmd string) (string, error) {
	if cmd == "" {
		cmd = "z"
	}
	t, ok := navTemplates[sh]
	if !ok {
		return "", fmt.Errorf("unsupported shell %q", sh)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, navData{Cmd: cmd}); err != nil {
		return "", fmt.Errorf("generate %s nav snippet: %w", sh, err)
	}
	return buf.String(), nil
}

var navTemplates = map[Shell]*template.Template{
	Bash: template.Must(template.New("nav-bash").Parse(navBashTemplate)),
	Zsh:  template.Must(template.New("nav-zsh").Parse(navZshTemplate)),
	Fish: template.Must(template.New("nav-fish").Parse(navFishTemplate)),
}

// __safu_z_add records the current directory on each change. The "changed since
// last" guard avoids over-counting when a hook fires on every prompt (bash).
const navRecorder = `__safu_z_add() {
  [ -n "$SAFU_DISABLE" ] && return
  [ "$PWD" = "$__safu_z_last" ] && return
  __safu_z_last="$PWD"
  command safu z --add "$PWD" >/dev/null 2>&1
}
`

const navJumpPosix = `{{.Cmd}}() {
  if [ "$#" -eq 0 ]; then
    builtin cd "$HOME"
    return
  fi
  local __safu_d
  __safu_d=$(command safu z --resolve -- "$@") && builtin cd "$__safu_d"
}
`

const navZshTemplate = `# safu smart navigation (zsh). Source from your rc file.
` + navRecorder + `autoload -Uz add-zsh-hook
add-zsh-hook chpwd __safu_z_add
` + navJumpPosix

const navBashTemplate = `# safu smart navigation (bash). Source from your rc file.
` + navRecorder + `PROMPT_COMMAND="__safu_z_add${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
` + navJumpPosix

const navFishTemplate = `# safu smart navigation (fish). Source from config.fish.
function __safu_z_add --on-variable PWD
  test -n "$SAFU_DISABLE"; and return
  command safu z --add "$PWD" >/dev/null 2>&1
end
function {{.Cmd}}
  if test (count $argv) -eq 0
    builtin cd $HOME
    return
  end
  set -l __safu_d (command safu z --resolve -- $argv)
  and builtin cd $__safu_d
end
`

// posixTemplate covers bash and zsh (identical POSIX function syntax here).
const posixTemplate = `# safu shell integration — source this from your rc file.
# Generated by ` + "`safu init`" + `. Set SAFU_DISABLE=1 to bypass entirely.
{{range .Commands}}
{{.}}() {
  if [ -z "$SAFU_DISABLE" ] && command -v safu >/dev/null 2>&1; then
    safu guard --as {{.}} -- "$@"
    __safu_rc=$?
    if [ "$__safu_rc" -eq {{$.ExitHandled}} ]; then
      return 0
    elif [ "$__safu_rc" -eq {{$.ExitBlock}} ]; then
      return {{$.ExitBlock}}
    fi
  fi
  command {{.}} "$@"
}
{{end}}`

const fishTemplate = `# safu shell integration — source this from config.fish.
# Generated by ` + "`safu init`" + `. Set SAFU_DISABLE=1 to bypass entirely.
{{range .Commands}}
function {{.}}
  if test -z "$SAFU_DISABLE"; and type -q safu
    safu guard --as {{.}} -- $argv
    set -l __safu_rc $status
    if test $__safu_rc -eq {{$.ExitHandled}}
      return 0
    else if test $__safu_rc -eq {{$.ExitBlock}}
      return {{$.ExitBlock}}
    end
  end
  command {{.}} $argv
end
{{end}}`
