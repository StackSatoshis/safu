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

// fixData drives the correction-helper hook templates.
type fixData struct {
	Aliases []string // function names, e.g. ["fix","wtf"]
}

// FixSnippet generates the correction-helper integration: a recorder that tees
// session stderr to a local file (so the previous command's output is captured
// AS IT HAPPENS — never by re-running it, invariant #4) plus per-command
// offset marks, and the fix/wtf functions that feed the stored record to
// `safu fix` and offer to run the correction.
//
// fish is not yet supported (its stderr handling differs); callers should fall
// back to a notice.
func FixSnippet(sh Shell, aliases []string) (string, error) {
	if len(aliases) == 0 {
		aliases = []string{"fix", "wtf"}
	}
	t, ok := fixTemplates[sh]
	if !ok {
		return "", fmt.Errorf("fix integration is not yet supported for %s", sh)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, fixData{Aliases: aliases}); err != nil {
		return "", fmt.Errorf("generate %s fix snippet: %w", sh, err)
	}
	return buf.String(), nil
}

var fixTemplates = map[Shell]*template.Template{
	Bash: template.Must(template.New("fix-bash").Parse(fixBashTemplate)),
	Zsh:  template.Must(template.New("fix-zsh").Parse(fixZshTemplate)),
}

// HistorySnippet generates the general shell-history integration (SPEC.md
// §8.2): a recorder that appends each command to safu's local history, and a
// Ctrl-R widget that opens safu's fuzzy history browser and inserts the chosen
// command on the line. bash/zsh only (fish deferred). Honors SAFU_DISABLE.
func HistorySnippet(sh Shell) (string, error) {
	switch sh {
	case Bash:
		return historyBashTemplate, nil
	case Zsh:
		return historyZshTemplate, nil
	default:
		return "", fmt.Errorf("history integration is not yet supported for %s", sh)
	}
}

// Recorder shared by bash and zsh: capture $? first, then the last command via
// fc, and record it (skipping empties and immediate repeats).
const historyRecorder = `__safu_hist_record() {
  local __safu_code=$?
  [ -n "$SAFU_DISABLE" ] && return $__safu_code
  local __safu_cmd
  __safu_cmd=$(fc -ln -1 2>/dev/null)
  __safu_cmd="${__safu_cmd#"${__safu_cmd%%[![:space:]]*}"}"
  if [ -z "$__safu_cmd" ] || [ "$__safu_cmd" = "$__safu_hist_last" ]; then
    return $__safu_code
  fi
  __safu_hist_last="$__safu_cmd"
  command safu history --add --exit "$__safu_code" --dir "$PWD" -- "$__safu_cmd" >/dev/null 2>&1
  return $__safu_code
}
`

const historyZshTemplate = `# safu shell history (zsh). Source from your rc file.
` + historyRecorder + `autoload -Uz add-zsh-hook
add-zsh-hook precmd __safu_hist_record
__safu_history_widget() {
  local __safu_sel
  __safu_sel=$(command safu history < /dev/tty) || return
  if [ -n "$__safu_sel" ]; then
    BUFFER="$__safu_sel"
    CURSOR=${#BUFFER}
  fi
  zle reset-prompt
}
zle -N __safu_history_widget
bindkey '^R' __safu_history_widget
`

const historyBashTemplate = `# safu shell history (bash). Source from your rc file.
` + historyRecorder + `PROMPT_COMMAND="__safu_hist_record${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
__safu_history_widget() {
  local __safu_sel
  __safu_sel=$(command safu history < /dev/tty)
  READLINE_LINE="$__safu_sel"
  READLINE_POINT=${#READLINE_LINE}
}
bind -x '"\C-r": __safu_history_widget'
`

// The recorder body is shared by bash and zsh. It tees stderr to a local log
// and __safu_fix_mark (run at each prompt) tracks the byte range of the last
// command's stderr. Capping keeps the log bounded.
const fixRecorder = `: "${SAFU_ERRLOG:=$HOME/.safu/session.err}"
mkdir -p "$(dirname "$SAFU_ERRLOG")" 2>/dev/null
exec 2> >(command tee -a "$SAFU_ERRLOG" >&2)
__safu_err_prev=0
__safu_err_cur=0
__safu_fix_mark() {
  __safu_err_prev=$__safu_err_cur
  __safu_err_cur=$(wc -c < "$SAFU_ERRLOG" 2>/dev/null || echo 0)
  if [ "$__safu_err_cur" -gt 1048576 ]; then
    : > "$SAFU_ERRLOG"
    __safu_err_prev=0
    __safu_err_cur=0
  fi
}
`

// The fix function body is shared; {{.}} is the function name.
const fixFunc = `{{range .Aliases}}{{.}}() {
  local __safu_code=$?
  local __safu_last __safu_err __safu_fix
  __safu_last=$(fc -ln -1 2>/dev/null)
  __safu_last="${__safu_last#"${__safu_last%%[![:space:]]*}"}"
  __safu_err=""
  if [ -f "$SAFU_ERRLOG" ] && [ "$__safu_err_cur" -gt "$__safu_err_prev" ]; then
    __safu_err=$(tail -c +"$((__safu_err_prev + 1))" "$SAFU_ERRLOG" 2>/dev/null | head -c "$((__safu_err_cur - __safu_err_prev))")
  fi
  __safu_fix=$(printf '%s' "$__safu_err" | command safu fix --first --exit "$__safu_code" -- "$__safu_last") || {
    echo "safu: no correction found" >&2
    return 1
  }
  printf 'fix: %s\n' "$__safu_fix" >&2
  if [ -n "$SAFU_FIX_YES" ]; then eval "$__safu_fix"; return $?; fi
  printf 'run it? [y/N] ' >&2
  local __safu_ans
  read -r __safu_ans
  case "$__safu_ans" in
    y | Y | yes | YES) eval "$__safu_fix" ;;
  esac
}
{{end}}`

const fixZshTemplate = `# safu correction helper (zsh). Source from your rc file.
` + fixRecorder + `autoload -Uz add-zsh-hook
add-zsh-hook precmd __safu_fix_mark
` + fixFunc

const fixBashTemplate = `# safu correction helper (bash). Source from your rc file.
` + fixRecorder + `PROMPT_COMMAND="__safu_fix_mark${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
` + fixFunc

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
