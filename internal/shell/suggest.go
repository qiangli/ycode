package shell

import (
	"fmt"
	"io"
)

// SuggestFunc is the agentmode hook that turns a raw command string into
// a list of hints. internal/shell/agentmode wires this in init() to
// avoid the import cycle.
//
// Inputs: the runtime (for context — cwd, available skills, etc.) and
// the raw command. Returns hints suitable for stderr printing.
type SuggestFunc func(rt *ShellRuntime, command string) []Hint

// Hint is the public hint shape used by both --suggest and --agent
// output augmentation. Mirrors the agentmode internal Hint with only
// the fields callers care about.
//
// Why is a one-line rationale (e.g. "AST-aware; skips comments/strings")
// that gives the agent reading the hint a reason to switch rather than
// stick with muscle-memory. Empty when the catalog entry doesn't supply one.
type Hint struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Message  string `json:"message"`
	Why      string `json:"why,omitempty"`

	// SkipOnSuccess marks the hint as suggestion-only-when-things-go-wrong:
	// the caller (cmd/ycode/shell_cmd.go and DispatchEnvelope) should
	// suppress it when the command exited 0. Pre-exec hints set this
	// when their suggestion is a "you might prefer yc <verb>" nudge
	// rather than a correctness warning — idiomatic exit-0 invocations
	// shouldn't carry repeated context-bloat hints.
	SkipOnSuccess bool `json:"skip_on_success,omitempty"`
}

var suggestFn SuggestFunc

// SetSuggestFunc is called by internal/shell/agentmode/init() to wire
// the hint-engine entry point.
func SetSuggestFunc(fn SuggestFunc) { suggestFn = fn }

// WriteSuggestions emits hint lines to w for the given command, without
// executing it. Returns an error only on write failure.
func WriteSuggestions(rt *ShellRuntime, command string, w io.Writer) error {
	hints := Suggestions(rt, command)
	if len(hints) == 0 {
		_, err := fmt.Fprintln(w, "(no hints)")
		return err
	}
	for _, h := range hints {
		if _, err := fmt.Fprintf(w, "# ycode hint [%s]: %s\n", h.Category, h.Message); err != nil {
			return err
		}
		if h.Why != "" {
			if _, err := fmt.Fprintf(w, "#   why: %s\n", h.Why); err != nil {
				return err
			}
		}
	}
	return nil
}

// Suggestions runs the registered SuggestFunc and returns its hints.
// When the agentmode package isn't loaded (no init), returns nil.
func Suggestions(rt *ShellRuntime, command string) []Hint {
	if suggestFn == nil {
		return nil
	}
	return suggestFn(rt, command)
}
