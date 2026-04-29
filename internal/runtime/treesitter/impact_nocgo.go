//go:build !cgo

package treesitter

import "context"

// Analyze returns ErrNoCGO when built without CGO.
func Analyze(ctx context.Context, parser *Parser, symbol, targetFile, workspaceRoot string) ([]Impact, error) {
	return nil, ErrNoCGO
}
