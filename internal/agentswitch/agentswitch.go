// Package agentswitch builds and guards the invocation that hands a ycode
// session over to another agentic CLI.
//
// It is the shared core of two paths that differ only in how they RUN the
// command, never in how they build it:
//
//   - the slash path (/agent, /tool) hands over the terminal — the user
//     becomes the other agent and returns here when it exits;
//   - the tool-call path runs it headlessly and brings the answer back into
//     the conversation.
//
// It lives in its own package because internal/tools must not import
// internal/cli. A helper hanging off *App could only serve the slash path.
//
// Everything here is pure: no process is started, no terminal is touched.
// Command() returns argv and Guard() returns an error, so both are testable
// without a TTY, a fleet host, or bashy on PATH.
package agentswitch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/api"
)

const (
	// DepthEnv bounds ycode → bashy → ycode → … recursion. Mirrors the
	// BASHY_MEET_DEPTH guard in coreutils/pkg/meet, for the same reason:
	// the recursion is otherwise unbounded and each level costs a real
	// agent session.
	DepthEnv = "YCODE_AGENT_DEPTH"

	// MeetDepthEnv is bashy's meeting guard. Switching agents from inside a
	// meeting turn forks the same way, so we refuse on it too — a
	// participant owes the meeting a turn, not a new session.
	MeetDepthEnv = "BASHY_MEET_DEPTH"

	// BinEnv overrides how the bashy binary is located.
	BinEnv = "YCODE_BASHY_BIN"

	maxDepth = 1
)

// Mode selects what the target inherits from this session.
type Mode string

const (
	// ModeCarry passes the current conversation as context. It is the
	// DEFAULT, and it is the entire reason to switch through ycode instead
	// of opening a terminal and running the tool yourself: the work comes
	// with you.
	ModeCarry Mode = "carry"

	// ModeFresh starts the target with no history — the explicit opt-out,
	// for when the next task has nothing to do with this one.
	ModeFresh Mode = "fresh"
)

// Request describes one switch.
type Request struct {
	// Agent selects a full agent (tool:model), a nickname, or a band (L3).
	// Exactly one of Agent or Tool must be set.
	Agent string

	// Tool selects a CLI by name and lets it use its own preconfigured
	// model and settings.
	Tool string

	// Instruction is what the target should DO. Required for a headless run
	// (bashy's -m); meaningless for an interactive one, where the human
	// drives. Kept separate from Context because they are different things:
	// passing the same string as both duplicated it on the wire.
	Instruction string

	// Context is the session history carried across (bashy's --context). This
	// is the payload that makes switching through ycode worth doing.
	Context string

	// Files are attached verbatim (bashy reads and appends their contents).
	Files []string

	// Mode defaults to ModeCarry.
	Mode Mode

	// Interactive requests a live session (the slash path). When false the
	// target runs headless and its output is captured (the tool-call path).
	Interactive bool

	// ReadOnly propagates this session's restriction downward. It is never
	// widened here: bashy owns the child's governance, and synthesizing
	// --yolo or --allow-premium from inside ycode would route around it.
	ReadOnly bool

	// Cwd is the working directory for the child. Empty means inherit.
	Cwd string

	// Timeout bounds a headless run. Ignored when Interactive.
	Timeout time.Duration
}

// Target is the resolved destination, for reporting what you switched into.
type Target struct {
	Agent api.FleetAgent // zero when switching by tool
	Tool  string         // set when switching by tool
}

// Label renders the target for a one-line message.
func (t Target) Label() string {
	if t.Tool != "" {
		return t.Tool
	}
	return t.Agent.Label()
}

// Resolve validates the request and resolves its target against the fleet
// catalog, without building a command or touching the environment.
func Resolve(r Request) (Target, error) {
	agent, tool := strings.TrimSpace(r.Agent), strings.TrimSpace(r.Tool)
	switch {
	case agent == "" && tool == "":
		return Target{}, fmt.Errorf("name an agent (/agent codex-gpt-5.5) or a tool (/tool codex)")
	case agent != "" && tool != "":
		return Target{}, fmt.Errorf("choose an agent or a tool, not both")
	case tool != "":
		name, err := api.ResolveFleetTool(tool)
		if err != nil {
			return Target{}, err
		}
		return Target{Tool: name}, nil
	default:
		a, err := api.ResolveFleetAgent(agent)
		if err != nil {
			return Target{}, err
		}
		return Target{Agent: a}, nil
	}
}

// Command builds the argv that runs the target through bashy.
//
// bashy is invoked rather than the tool directly, deliberately: it owns agent
// resolution, the credential firewall, sandboxing and room membership. Calling
// `codex` straight from here would bypass all of it and make ycode a second,
// weaker launcher.
func Command(r Request) ([]string, Target, error) {
	target, err := Resolve(r)
	if err != nil {
		return nil, Target{}, err
	}
	bin, err := BashyPath()
	if err != nil {
		return nil, Target{}, err
	}

	argv := []string{bin, "chat"}
	if target.Tool != "" {
		argv = append(argv, "--tool", target.Tool)
	} else {
		argv = append(argv, "--agent", target.Agent.Name)
	}

	if r.Interactive {
		argv = append(argv, "-i")
	}
	if r.ReadOnly {
		argv = append(argv, "--read-only")
	}
	if cwd := strings.TrimSpace(r.Cwd); cwd != "" {
		argv = append(argv, "--cwd", cwd)
	}
	if !r.Interactive && r.Timeout > 0 {
		argv = append(argv, "--timeout", r.Timeout.String())
	}

	if r.mode() == ModeCarry {
		if ctx := strings.TrimSpace(r.Context); ctx != "" {
			argv = append(argv, "--context", ctx)
		}
		for _, f := range r.Files {
			if f = strings.TrimSpace(f); f != "" {
				argv = append(argv, "--file", f)
			}
		}
	}

	// A headless run needs an instruction to act on; without one bashy has
	// nothing to answer and would fail at launch.
	if !r.Interactive {
		if strings.TrimSpace(r.Instruction) == "" {
			return nil, Target{}, fmt.Errorf("a headless switch needs an instruction describing what the agent should do")
		}
		argv = append(argv, "-m", r.Instruction)
	}

	return argv, target, nil
}

func (r Request) mode() Mode {
	if r.Mode == ModeFresh {
		return ModeFresh
	}
	return ModeCarry
}

// Guard reports why this session may not switch, or nil when it may.
//
// Every check fails loud. A switch that silently does nothing is the worst
// outcome: the user believes their context moved when it did not.
func Guard() error {
	if d := depth(DepthEnv); d >= maxDepth {
		return fmt.Errorf("refusing to switch agents from inside a switched agent (%s=%d).\n"+
			"      Exit back to the outer session first — nesting these is unbounded, "+
			"and each level is a real agent session", DepthEnv, d)
	}
	if d := depth(MeetDepthEnv); d >= 1 {
		return fmt.Errorf("refusing to switch agents from inside a meeting (%s=%d).\n"+
			"      A participant owes the meeting a turn, not a new session", MeetDepthEnv, d)
	}
	if runtime.GOOS == "windows" {
		return fmt.Errorf("switching agents is not supported on Windows: " +
			"the interactive path needs a pty, which bashy does not provide there")
	}
	if _, err := BashyPath(); err != nil {
		return err
	}
	return nil
}

// ChildEnv returns the environment additions the child needs — currently just
// the depth stamp, which must be set on the CHILD rather than via os.Setenv:
// mutating this process's environment would leak the guard into every later
// tool call in the session.
func ChildEnv() []string {
	return []string{fmt.Sprintf("%s=%d", DepthEnv, depth(DepthEnv)+1)}
}

func depth(key string) int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// BashyPath locates the bashy binary: an explicit override first, then PATH,
// then the documented install location. Absence is an error naming the fix,
// never a silent fallback to running the tool directly.
func BashyPath() (string, error) {
	if custom := strings.TrimSpace(os.Getenv(BinEnv)); custom != "" {
		if p, err := exec.LookPath(custom); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("%s=%s is not executable", BinEnv, custom)
	}
	if p, err := exec.LookPath("bashy"); err == nil {
		return p, nil
	}
	for _, dir := range []string{os.Getenv("DHNT_BIN_DIR"), defaultBinDir()} {
		if dir == "" {
			continue
		}
		cand := filepath.Join(dir, "bashy")
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("bashy not found on PATH.\n"+
		"      Switching agents runs them through bashy, which owns agent resolution and sandboxing.\n"+
		"      Install it (dhnt: script/install-user-bins.sh) or set %s to its path", BinEnv)
}

func defaultBinDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin")
}
