package bundle

import "github.com/StackSatoshis/safu/internal/shell"

// All blocks below are hand-authored shell — no external tools, no network —
// so they pass safu's privacy bar trivially (§11.3).

func shellOptions(sh shell.Shell) string {
	switch sh {
	case shell.Zsh:
		return `# safu: sane shell options
setopt HIST_IGNORE_ALL_DUPS HIST_SAVE_NO_DUPS HIST_REDUCE_BLANKS
setopt NO_CLOBBER
setopt INTERACTIVE_COMMENTS`
	case shell.Bash:
		return `# safu: sane shell options
HISTCONTROL=ignoreboth
shopt -s histappend
set -o noclobber`
	}
	return ""
}

// aliasesBlock is POSIX-compatible (bash + zsh).
func aliasesBlock(sh shell.Shell) string {
	return `# safu: curated aliases
alias ll='ls -lah'
alias la='ls -A'
alias l='ls -CF'
alias gs='git status'
alias gd='git diff'
alias ga='git add'
alias gc='git commit'
alias gl='git log --oneline -20'
mkcd() { mkdir -p "$1" && cd "$1"; }`
}

func promptBlock(sh shell.Shell) string {
	switch sh {
	case shell.Zsh:
		return `# safu: clean git-aware prompt (local git only)
__safu_git_branch() { git rev-parse --abbrev-ref HEAD 2>/dev/null; }
setopt PROMPT_SUBST
PROMPT='%~$(__b=$(__safu_git_branch); [ -n "$__b" ] && echo " ($__b)") %# '`
	case shell.Bash:
		return `# safu: clean git-aware prompt (local git only)
__safu_git_branch() { git rev-parse --abbrev-ref HEAD 2>/dev/null; }
PS1='\w$(__b=$(__safu_git_branch); [ -n "$__b" ] && echo " ($__b)") \$ '`
	}
	return ""
}

func teachingBlock(sh shell.Shell) string {
	return `# safu: beginner hints
#   - destructive commands are guarded; deletes go to trash (safu undo to restore)
#   - 'fix' or 'wtf' suggests a correction for your last command
#   - 'z <dir>' jumps to a frequently-used directory
#   - 'safu log' shows what safu did
alias safu-cheatsheet='safu help'`
}
