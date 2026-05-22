package main

import (
	"testing"

	"github.com/qiangli/ycode/internal/selfinit"
)

func TestResolveServerPort_DefaultIsSelfInitConstant(t *testing.T) {
	// resolveServerPort with no YCODE_PORT env must return the canonical
	// selfinit.DefaultPort. A stale ~/.agents/ycode/serve.port file is
	// read elsewhere (detectServer) only to find an already-running
	// server; fresh starts always use the constant. This pins the
	// clean-break behavior: bumping selfinit.DefaultPort changes the
	// default everywhere without per-call-site updates.
	t.Setenv("YCODE_PORT", "")
	if got := resolveServerPort(); got != selfinit.DefaultPort {
		t.Errorf("resolveServerPort() = %d, want selfinit.DefaultPort (%d)", got, selfinit.DefaultPort)
	}
}

func TestResolveServerPort_EnvOverride(t *testing.T) {
	t.Setenv("YCODE_PORT", "55555")
	if got := resolveServerPort(); got != 55555 {
		t.Errorf("resolveServerPort() = %d, want 55555 (YCODE_PORT override)", got)
	}
}

func TestResolveServerPort_InvalidEnvFallsBack(t *testing.T) {
	t.Setenv("YCODE_PORT", "not-a-number")
	if got := resolveServerPort(); got != selfinit.DefaultPort {
		t.Errorf("resolveServerPort() with garbage env = %d, want selfinit.DefaultPort (%d)", got, selfinit.DefaultPort)
	}
}
