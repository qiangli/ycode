package shell

import (
	"errors"
	"os"
	"sync"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/permission"
)

// Options configures a ShellRuntime.
type Options struct {
	// WorkDir is the initial working directory. Empty means os.Getwd().
	WorkDir string

	// Permission is the mode string ("read-only" / "workspace-write" /
	// "danger-full-access"). Empty defaults to "danger-full-access" —
	// the user is the operator. Validators that the agent-mode bash
	// tool applies are not enforced in shell mode.
	Permission string

	// Registry holds slash commands resolvable via the `/` sentinel.
	// May be nil; in that case slash commands all fail with
	// "no slash registry configured" until callers wire one in.
	Registry *commands.Registry

	// Skills resolves the `@` sentinel. May be nil; @-invocations then
	// fail with "no skill resolver configured".
	Skills SkillResolver

	// Provider drives the `!` and `?` sentinels. May be nil; in that
	// case those sentinels return ErrNoProvider with a helpful message.
	Provider api.Provider

	// Model is the model ID (or alias) the provider should use. Empty
	// falls back to the provider's default during request build.
	Model string
}

// ShellRuntime owns the per-shell-process state: session (bash interpreter
// runner, working directory), the slash and skill registries, and the
// permission mode. It is constructed once per `ycode shell` invocation,
// passed to the dispatcher and the TUI, and closed on exit.
type ShellRuntime struct {
	opts     Options
	session  *bash.ShellSession
	permMode permission.Mode

	// historyMu guards lastCmd / lastOutput, which feed the `!` agent
	// sentinel as shell context.
	historyMu  sync.Mutex
	lastCmd    string
	lastOutput string
}

// New constructs a ShellRuntime from Options.
func New(opts Options) (*ShellRuntime, error) {
	if opts.WorkDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		opts.WorkDir = wd
	}
	mode := permission.DangerFullAccess
	if opts.Permission != "" {
		mode = permission.ParseMode(opts.Permission)
	}

	return &ShellRuntime{
		opts:     opts,
		session:  bash.NewShellSession(opts.WorkDir),
		permMode: mode,
	}, nil
}

// Session returns the underlying bash session (cwd + persistent runner once
// Step 3 wires it).
func (r *ShellRuntime) Session() *bash.ShellSession { return r.session }

// Registry returns the slash-command registry, or nil if none was wired.
func (r *ShellRuntime) Registry() *commands.Registry { return r.opts.Registry }

// Skills returns the skill resolver, or nil if none was wired.
func (r *ShellRuntime) Skills() SkillResolver { return r.opts.Skills }

// PermMode returns the permission mode this shell runs at.
func (r *ShellRuntime) PermMode() permission.Mode { return r.permMode }

// WorkDir returns the current working directory of the shell session.
func (r *ShellRuntime) WorkDir() string {
	if r.session == nil {
		return r.opts.WorkDir
	}
	return r.session.WorkDir()
}

// Close releases any per-runtime resources. Currently a no-op; reserved
// for the persistent-runner shutdown in Step 3 and PTY cleanup in Step 7.
func (r *ShellRuntime) Close() error {
	if r == nil {
		return errors.New("ShellRuntime: nil receiver")
	}
	return nil
}

// cloneAt returns a fresh ShellRuntime with the same auxiliaries
// (registry, skills, provider, permission) but a new bash session rooted
// at workDir. Used by HTTP MCP callers that need per-call work-dir
// isolation without mutating the shared runtime; safe for concurrent use
// because each clone owns its own session.
func (r *ShellRuntime) cloneAt(workDir string) (*ShellRuntime, error) {
	opts := r.opts
	opts.WorkDir = workDir
	return New(opts)
}

// Provider returns the configured LLM provider, or nil when none is
// wired. The dispatcher uses this for `!`/`?` sentinels.
func (r *ShellRuntime) Provider() api.Provider { return r.opts.Provider }

// Model returns the model ID/alias to use for `!`/`?` requests.
func (r *ShellRuntime) Model() string { return r.opts.Model }

// RecordHistory updates the lastCmd / lastOutput shell-context that the
// `!` sentinel attaches to its system prompt. Called by the dispatcher
// after each successful bash run.
func (r *ShellRuntime) RecordHistory(cmd, output string) {
	r.historyMu.Lock()
	r.lastCmd = cmd
	r.lastOutput = output
	r.historyMu.Unlock()
}

// History returns a snapshot of the most-recent command and its output.
func (r *ShellRuntime) History() (cmd, output string) {
	r.historyMu.Lock()
	defer r.historyMu.Unlock()
	return r.lastCmd, r.lastOutput
}
