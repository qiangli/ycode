// The treesitter AST engine now lives in the shared AgentOS hub at
// github.com/qiangli/coreutils/pkg/treesitter, so bashy/ycode/outpost run
// one implementation. This file re-exports that engine under the original
// import path, keeping every in-tree caller (and this package's MCP server
// in mcpserver.go) compiling unchanged. The MCP adapter stays here because
// it depends on ycode's internal mcp package, which the hub must not import.
package treesitter

import cts "github.com/qiangli/coreutils/pkg/treesitter"

// Engine types — aliases preserve identity and full method sets, so a
// *Parser here IS a *cts.Parser.
type (
	Tree   = cts.Tree
	Parser = cts.Parser
	Match  = cts.Match
	Symbol = cts.Symbol
	Impact = cts.Impact
)

// Engine functions, forwarded.
var (
	NewParser          = cts.NewParser
	ExtractSymbols     = cts.ExtractSymbols
	WalkNodes          = cts.WalkNodes
	GetLanguage        = cts.GetLanguage
	SupportedLanguages = cts.SupportedLanguages
	IsSupported        = cts.IsSupported
	Analyze            = cts.Analyze
	Search             = cts.Search
	SearchText         = cts.SearchText
)
