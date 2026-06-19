package guard

// Action is the resolved outcome of a Decision under the user's options.
type Action int

const (
	Approve Action = iota // proceed with the real command
	BlockIt               // refuse
)

// Prompter asks the user to confirm a warned command. Injecting it keeps Decide
// testable without a TTY.
type Prompter interface {
	Confirm(prompt string) bool
}

// Options carries the override flags and runtime context for a decision.
type Options struct {
	Force       bool // typed --force: override a block
	Yes         bool // --yes: auto-confirm a warn
	Interactive bool // a TTY is available to prompt on
}

// Decide maps a classified Decision plus user options to an Action.
//
//   - Safe  -> Approve.
//   - Warn  -> Approve if --yes, else prompt when interactive (default No),
//     else BlockIt (no TTY and not forced -> the safe choice).
//   - Block -> Approve only if --force was typed, else BlockIt.
func Decide(d Decision, opt Options, p Prompter) Action {
	switch d.Risk {
	case Safe:
		return Approve
	case Block:
		if opt.Force {
			return Approve
		}
		return BlockIt
	case Warn:
		if opt.Yes || opt.Force {
			return Approve
		}
		if opt.Interactive && p != nil && p.Confirm("Proceed?") {
			return Approve
		}
		return BlockIt
	default:
		return BlockIt
	}
}
