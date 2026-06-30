package ycode

import (
	"github.com/qiangli/ycode/internal/runtime/embedding"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/tools"
)

// PermissionMode mirrors internal/runtime/permission.Mode for callers that
// drive Agent permission decisions without dipping into internal packages.
type PermissionMode = permission.Mode

// Re-exports of the three permission tiers. ReadOnly excludes write/exec
// tools; WorkspaceWrite allows file writes within allowed dirs; DangerFullAccess
// is unrestricted (bash, network, MCP).
const (
	PermReadOnly         PermissionMode = permission.ReadOnly
	PermWorkspaceWrite   PermissionMode = permission.WorkspaceWrite
	PermDangerFullAccess PermissionMode = permission.DangerFullAccess
)

// PermissionResolver returns the live permission mode for a tool invocation.
type PermissionResolver = tools.PermissionResolver

// PermissionPrompter decides whether an elevated-permission tool may run.
// Return true to allow, false to deny. Errors propagate to the caller.
type PermissionPrompter = tools.PermissionPrompter

// EmbeddingProvider produces vector embeddings for arbitrary text. Used by
// (*Agent).Embed and EmbedBatch when set via WithEmbeddingProvider.
type EmbeddingProvider = embedding.Provider

// WithoutBuiltinTools suppresses registration of every built-in tool
// (bash, read_file, write_file, edit_file, glob_search, grep_search, Agent,
// Skill, ToolSearch, and all deferred tools).
//
// Use this when embedding ycode as a chat backbone for a third-party app that
// must NOT expose host-machine shell or file access. After construction,
// register the host's domain tools via (*Agent).Registry().Register(...).
//
// Mutually exclusive with WithBuiltinAllowlist — whichever is last wins.
func WithoutBuiltinTools() Option {
	return func(a *Agent) error {
		a.skipBuiltins = true
		a.builtinAllowlist = nil
		return nil
	}
}

// WithBuiltinAllowlist registers ONLY the named built-in tools. Pass an empty
// slice to register nothing (equivalent to WithoutBuiltinTools). Pass tool
// names exactly as they appear in (*Registry).Names() — see
// AllowedToolsForMode and AllowedToolsForAgent for curated starting points.
//
// Mutually exclusive with WithoutBuiltinTools — whichever is last wins.
func WithBuiltinAllowlist(names []string) Option {
	return func(a *Agent) error {
		a.skipBuiltins = false
		// Copy so callers can mutate their slice without affecting the Agent.
		a.builtinAllowlist = append([]string(nil), names...)
		if a.builtinAllowlist == nil {
			a.builtinAllowlist = []string{}
		}
		return nil
	}
}

// WithPermissionResolver installs a callback that returns the live permission
// mode on every tool invocation. The default (no resolver) allows every tool.
//
// Hosts that want a globally read-only Agent can pass:
//
//	WithPermissionResolver(func() PermissionMode { return PermReadOnly })
func WithPermissionResolver(r PermissionResolver) Option {
	return func(a *Agent) error {
		a.permResolver = r
		return nil
	}
}

// WithPermissionPrompter installs a callback that decides whether a tool
// invocation exceeding the current mode is allowed. Returning false denies
// without prompting a human, which is the right shape for headless hosts.
//
// A nil prompter combined with a restrictive resolver produces hard denial.
func WithPermissionPrompter(p PermissionPrompter) Option {
	return func(a *Agent) error {
		a.permPrompter = p
		return nil
	}
}

// WithEmbeddingProvider configures the provider used by (*Agent).Embed and
// EmbedBatch. When unset, the Agent lazily falls back to
// embedding.DetectProvider() — the same env-var precedence ladder used
// internally (YCODE_EMBEDDING_API → TF-IDF).
//
// Chat and embeddings are independent providers; coupling them would force a
// single model family on both.
func WithEmbeddingProvider(p EmbeddingProvider) Option {
	return func(a *Agent) error {
		a.embedProvider = p
		return nil
	}
}
