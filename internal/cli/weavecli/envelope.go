// Package weavecli holds the agent-friendly CLI conventions every
// `ycode weave` subverb shares: versioned-envelope marshaling, stable
// exit-code constants, tty/agent-mode detection, and the YCODE_AGENT
// switch that flips all the user-visible defaults to machine-friendly.
//
// Lives outside cmd/ycode so internal callers (autopilot worker,
// test scaffolding) can also produce envelope-shaped output without
// importing the cobra command tree.
package weavecli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// SchemaVersion is stamped into every envelope's schema_version field.
// Bump when output shape changes; clients pin against the version they
// tested.
const SchemaVersion = "loom-v2"

// Stable exit codes. See docs/loom-v2-plan.md "Agent-friendly CLI →
// Stable exit codes" table.
const (
	ExitOK            = 0
	ExitGenericFail   = 1
	ExitInvalidArg    = 2
	ExitPrecondFail   = 3
	ExitStateConflict = 4
	ExitDepUnhealthy  = 5
)

// Envelope is the structured response shape returned in --json mode.
// All fields except SchemaVersion / Command / Status are optional.
type Envelope struct {
	SchemaVersion string         `json:"schema_version"`
	Command       string         `json:"command"`
	Status        string         `json:"status"` // ok | error | partial
	Result        any            `json:"result,omitempty"`
	Error         *EnvelopeError `json:"error,omitempty"`
	Hints         []Hint         `json:"hints,omitempty"`
}

// EnvelopeError carries the agent-actionable error code (matching one
// of the Exit* constants by suffix) and a human-readable message.
type EnvelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Hint is a structured suggestion emitted by the agent-mode engine
// (e.g. "you used `weave start` outside auto-attach; consider --tool
// flag instead of trailing --").
type Hint struct {
	Why     string `json:"why"`
	Suggest string `json:"suggest"`
}

// OutputMode controls how a subverb renders its result.
type OutputMode int

const (
	OutputAuto  OutputMode = iota // auto-detect from tty
	OutputJSON                    // --json: emit envelope
	OutputPlain                   // --plain: no ANSI, no spinners
	OutputQuiet                   // --quiet: final result line only
)

// IsAgent reports whether YCODE_AGENT in the environment requests
// agent defaults (forces --json + --plain + no prompts + no spinners).
func IsAgent() bool {
	v := os.Getenv("YCODE_AGENT")
	switch v {
	case "", "0", "false", "no":
		return false
	}
	return true
}

// StdoutIsTTY reports whether stdout is a character device (terminal).
// False for pipes, redirects, capture-by-subprocess, and YCODE_AGENT=1.
func StdoutIsTTY() bool {
	if IsAgent() {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// ResolveOutputMode picks the right OutputMode from the per-call flags
// + the auto-detection rules. Precedence: --json > --plain > --quiet >
// auto. YCODE_AGENT=1 forces --json regardless of other flags so
// agents always get structured output.
func ResolveOutputMode(jsonFlag, plainFlag, quietFlag bool) OutputMode {
	if IsAgent() || jsonFlag {
		return OutputJSON
	}
	if plainFlag {
		return OutputPlain
	}
	if quietFlag {
		return OutputQuiet
	}
	return OutputAuto
}

// EmitOK writes a status=ok envelope to w and returns ExitOK. The
// helper exists so subverbs don't repeat the marshal-or-print
// boilerplate at every return path.
func EmitOK(w io.Writer, mode OutputMode, command string, result any) int {
	if mode == OutputJSON {
		_ = encodeEnvelope(w, Envelope{
			SchemaVersion: SchemaVersion,
			Command:       command,
			Status:        "ok",
			Result:        result,
		})
		return ExitOK
	}
	// Plain/quiet/auto: caller-rendered output happens before EmitOK;
	// the helper only returns the exit code in non-JSON mode.
	return ExitOK
}

// EmitError writes a status=error envelope (in JSON mode) or a plain-
// text "<command>: <message>" line (otherwise) to stderr and returns
// the given exit code. Pairs with cobra's RunE return path so subverbs
// uniformly turn errors into the right exit shape.
func EmitError(stderr io.Writer, mode OutputMode, command string, code int, err error) int {
	if mode == OutputJSON {
		_ = encodeEnvelope(stderr, Envelope{
			SchemaVersion: SchemaVersion,
			Command:       command,
			Status:        "error",
			Error: &EnvelopeError{
				Code:    codeToString(code),
				Message: err.Error(),
			},
		})
		return code
	}
	fmt.Fprintf(stderr, "%s: %s\n", command, err.Error())
	return code
}

func codeToString(code int) string {
	switch code {
	case ExitInvalidArg:
		return "invalid_arg"
	case ExitPrecondFail:
		return "precondition_failed"
	case ExitStateConflict:
		return "state_conflict"
	case ExitDepUnhealthy:
		return "dependency_unhealthy"
	case ExitOK:
		return "ok"
	default:
		return "generic_failure"
	}
}

func encodeEnvelope(w io.Writer, e Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(e)
}
