//go:build !cgo

package treesitter

import "context"

// Search returns ErrNoCGO when built without CGO.
func Search(ctx context.Context, parser *Parser, source []byte, lang, pattern, file string) ([]Match, error) {
	return nil, ErrNoCGO
}

// SearchText returns ErrNoCGO when built without CGO.
func SearchText(ctx context.Context, parser *Parser, source []byte, lang, pattern, file string) ([]Match, error) {
	return nil, ErrNoCGO
}
