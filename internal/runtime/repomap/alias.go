// The repomap engine now lives in the shared AgentOS hub at
// github.com/qiangli/coreutils/pkg/repomap. This file re-exports it under
// the original import path so in-tree callers and this package's MCP server
// (mcpserver.go, which depends on ycode's internal mcp package) compile
// unchanged. The corpus-coupled e2e relevance test stays in this repo
// because it asserts ycode's own source files rank top.
package repomap

import crm "github.com/qiangli/coreutils/pkg/repomap"

// MaxTokenBudget is the default output budget.
const MaxTokenBudget = crm.MaxTokenBudget

// Engine types — aliases preserve identity and method sets.
type (
	Symbol    = crm.Symbol
	FileEntry = crm.FileEntry
	RepoMap   = crm.RepoMap
	Options   = crm.Options
)

// Engine functions, forwarded.
var (
	DefaultOptions   = crm.DefaultOptions
	GenerateForFiles = crm.GenerateForFiles
	Generate         = crm.Generate
)
