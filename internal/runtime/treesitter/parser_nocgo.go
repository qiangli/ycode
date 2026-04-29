//go:build !cgo

package treesitter

import "context"

// Tree represents a parsed source file (nocgo stub).
type Tree struct {
	Source []byte
	Lang   string
}

// Parser is a no-op stub when built without CGO.
type Parser struct{}

// NewParser creates a stub parser that returns ErrNoCGO on all operations.
func NewParser() *Parser {
	return &Parser{}
}

// Parse returns ErrNoCGO when built without CGO.
func (p *Parser) Parse(ctx context.Context, source []byte, lang string) (*Tree, error) {
	return nil, ErrNoCGO
}

// ExtractSymbols returns nil when built without CGO.
func ExtractSymbols(tree *Tree, file string) []Symbol {
	return nil
}

// WalkNodes is a no-op when built without CGO.
func WalkNodes(node any, fn func(any) bool) {}
