// The codegraph engine (gfy-backed code graph build/load/mirror) now lives
// in the shared AgentOS hub at github.com/qiangli/coreutils/pkg/codegraph.
// This file re-exports it under the original import path so in-tree callers
// and this package's MCP server (mcpserver.go, which depends on ycode's
// internal mcp package) compile unchanged.
package codegraph

import ccg "github.com/qiangli/coreutils/pkg/codegraph"

// DefaultCachePath is the on-disk graph cache location.
const DefaultCachePath = ccg.DefaultCachePath

// Engine types — aliases preserve identity, method sets, and interfaces.
type (
	GraphContext = ccg.GraphContext
	Stats        = ccg.Stats
	ProgressFunc = ccg.ProgressFunc
	Manager      = ccg.Manager
	MirrorSink   = ccg.MirrorSink
)

// Engine functions, forwarded.
var (
	Build             = ccg.Build
	BuildWithProgress = ccg.BuildWithProgress
	Load              = ccg.Load
	CachePath         = ccg.CachePath
	NewManager        = ccg.NewManager
)
