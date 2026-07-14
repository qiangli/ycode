// Package unattended detects non-interactive execution contexts such as
// weave-orchestrated workspaces or CI pipelines. It is the single source
// of truth for "do not prompt the user" decisions across ycode.
package unattended

import (
	"context"
	"os"
)

type ctxKey struct{}

// WithValue returns a context that explicitly marks the execution as
// attended or unattended. Commands should call this when a --no-interactive
// or --yes flag is passed.
func WithValue(ctx context.Context, unattended bool) context.Context {
	return context.WithValue(ctx, ctxKey{}, unattended)
}

// IsUnattended reports whether ycode is running in an unattended context.
// It returns true when the context is marked unattended, or when the process
// environment indicates a non-interactive orchestrated workspace.
func IsUnattended(ctx context.Context) bool {
	if ctx != nil {
		if v, ok := ctx.Value(ctxKey{}).(bool); ok && v {
			return true
		}
	}
	switch os.Getenv("YCODE_UNATTENDED") {
	case "1", "true", "yes":
		return true
	}
	if os.Getenv("WEAVE_ID") != "" {
		return true
	}
	if os.Getenv("WEAVE_WORKSPACE") != "" {
		return true
	}
	if os.Getenv("CI") == "true" {
		return true
	}
	return false
}
