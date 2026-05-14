package wrap

import (
	"path/filepath"
	"sort"
	"strings"
)

// Profile is the per-foreign-agent configuration applied when a known
// agent is wrapped. Profiles populate sensible defaults for things the
// user would otherwise enumerate by hand: the long tail of language-
// specific shell-outs (python/pip/ruff for Aider, node/npm/npx for
// Claude Code, ...), the permission ceiling, and which runtime hooks
// (Python sitecustomize, Node --require) to install for bypass closure.
//
// Profile keys align 1:1 with internal/selfinit Tool.Name() values
// ("claude", "opencode", "codex", "aider", "gemini") so the same agent
// identifier works across `ycode init` and `ycode wrap`.
type Profile struct {
	// Name is the lookup key. Must match selfinit Tool.Name().
	Name string

	// Description shows up in `--help` and the smoke matrix.
	Description string

	// CmdAliases is the set of agent argv[0] basenames this profile
	// matches against for auto-detection. Defaults to []string{Name}
	// when empty. Lets `ycode wrap claude-code` and `ycode wrap claude`
	// both resolve to the "claude" profile.
	CmdAliases []string

	// ExtraShims appends to defaultShims. Each must be a basename.
	// Duplicates with defaultShims are de-duplicated in Apply.
	ExtraShims []string

	// PermissionDefault is the suggested ceiling for this agent. CLI
	// --permission overrides it; if both are empty, wrap.Run keeps its
	// hard-coded default.
	PermissionDefault string

	// RuntimeHooks lists languages whose process-internal patcher
	// should be injected ("python", "node"). Honored by Piece D; empty
	// in Piece B means no hooks even when the runtime is installed.
	RuntimeHooks []string
}

// AgentProfiles is the built-in registry. Keys are the canonical names
// returned by ResolveProfile. New foreign agents drop in here with a
// single entry — there is no init() or registration ceremony so the
// table reads as data, not as code.
//
// Coverage rationale per agent comes from inspection of each tool's
// public source/release notes; entries should stay narrow (high-signal
// shell-outs the agent uses on hot paths), not aspirational.
var AgentProfiles = map[string]*Profile{
	"claude": {
		Name:              "claude",
		Description:       "Anthropic Claude Code CLI (Bun-compiled). PATH-shim + MCP coverage only; Bun runtime does not honor NODE_OPTIONS=--require.",
		CmdAliases:        []string{"claude", "claude-code"},
		ExtraShims:        []string{"node", "npx", "tsc", "eslint", "prettier", "yarn", "pnpm"},
		PermissionDefault: "workspace-write",
		// Claude Code ships as a Bun-compiled single binary. Bun ignores
		// NODE_OPTIONS=--require, so the Node language hook we install
		// for other agents (opencode, gemini) silently no-ops here. The
		// PATH shim still catches bash/git/rg/etc. — the supported
		// integration path is "PATH shim + MCP" (see .mcp.json at the
		// ycode repo root, or run `ycode init --register-foreign-agents`
		// to seed the user-global Claude config). Leave nil so we don't
		// pretend coverage we don't have; wrap.go emits a one-line
		// stderr notice when this profile resolves.
		RuntimeHooks: nil,
	},
	"opencode": {
		Name:              "opencode",
		Description:       "opencode CLI (Bun/Node).",
		CmdAliases:        []string{"opencode"},
		ExtraShims:        []string{"node", "bun", "bunx", "npx", "tsc", "eslint", "prettier"},
		PermissionDefault: "workspace-write",
		RuntimeHooks:      []string{"node"},
	},
	"codex": {
		Name:              "codex",
		Description:       "OpenAI Codex CLI (Rust+Node).",
		CmdAliases:        []string{"codex"},
		ExtraShims:        []string{"node", "npx", "python3"},
		PermissionDefault: "workspace-write",
		// Codex is Rust-with-Node-helpers; hooking the Node helper is
		// low value. Leave empty until smoke shows real bypass volume.
		RuntimeHooks: nil,
	},
	"aider": {
		Name:              "aider",
		Description:       "Aider Python CLI.",
		CmdAliases:        []string{"aider"},
		ExtraShims:        []string{"python", "python3", "pip", "pip3", "ruff", "black", "pre-commit", "mypy", "pytest"},
		PermissionDefault: "workspace-write",
		RuntimeHooks:      []string{"python"},
	},
	"gemini": {
		Name:              "gemini",
		Description:       "Google Gemini CLI (Node).",
		CmdAliases:        []string{"gemini", "gemini-cli"},
		ExtraShims:        []string{"node", "npx"},
		PermissionDefault: "workspace-write",
		RuntimeHooks:      []string{"node"},
	},
}

// ResolveProfile picks the profile to apply for an invocation.
//
// Precedence:
//  1. explicit non-empty name — looked up directly in AgentProfiles.
//     An unknown explicit name returns (nil, false) so the caller can
//     surface a clean error rather than silently falling back.
//  2. argv[0] basename auto-detect — matches CmdAliases (or Name when
//     CmdAliases is empty) for each profile.
//  3. no match — returns (nil, false). The caller proceeds with
//     defaults; runtime hooks stay off.
func ResolveProfile(name string, agentArgs []string) (*Profile, bool) {
	if name != "" {
		p, ok := AgentProfiles[name]
		return p, ok
	}
	if len(agentArgs) == 0 {
		return nil, false
	}
	base := strings.ToLower(filepath.Base(agentArgs[0]))
	// Strip a trailing ".exe" so Windows naming doesn't break detection.
	base = strings.TrimSuffix(base, ".exe")
	for _, p := range AgentProfiles {
		aliases := p.CmdAliases
		if len(aliases) == 0 {
			aliases = []string{p.Name}
		}
		for _, alias := range aliases {
			if strings.ToLower(alias) == base {
				return p, true
			}
		}
	}
	return nil, false
}

// Apply merges profile defaults into opts, mutating it in place.
// Caller-provided CLI flags always win — Apply only fills empty
// fields. ExtraShims accumulates (profile + CLI), then de-duplicates
// while preserving order so the user sees consistent ordering in
// debug output.
func (p *Profile) Apply(opts *Options) {
	if p == nil || opts == nil {
		return
	}
	if opts.Permission == "" && p.PermissionDefault != "" {
		opts.Permission = p.PermissionDefault
	}
	if len(p.ExtraShims) > 0 {
		merged := make([]string, 0, len(p.ExtraShims)+len(opts.ExtraShims))
		merged = append(merged, p.ExtraShims...)
		merged = append(merged, opts.ExtraShims...)
		opts.ExtraShims = dedupPreserveOrder(merged)
	}
	// Only fill RuntimeHooks when the caller didn't express an
	// intent. nil means "auto — follow the profile"; an empty but
	// non-nil slice means "the user explicitly opted out" and Apply
	// must leave it as-is. cmd/ycode/wrap.go's parseRuntimeHooks
	// produces these two shapes from --runtime-hooks=auto vs =off.
	if len(p.RuntimeHooks) > 0 && opts.RuntimeHooks == nil {
		opts.RuntimeHooks = append([]string{}, p.RuntimeHooks...)
	}
}

func dedupPreserveOrder(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ProfileNames returns the registered profile keys sorted alphabetically.
// Used by `ycode wrap --help` to list known --profile values and by the
// smoke script to iterate the matrix rows.
func ProfileNames() []string {
	names := make([]string, 0, len(AgentProfiles))
	for k := range AgentProfiles {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
