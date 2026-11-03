package unattended

import (
	"context"
	"testing"
)

func TestIsUnattended_Context(t *testing.T) {
	t.Setenv("YCODE_UNATTENDED", "")
	t.Setenv("WEAVE_ID", "")
	t.Setenv("WEAVE_WORKSPACE", "")
	ctx := context.Background()
	if IsUnattended(ctx) {
		t.Error("expected not unattended by default")
	}
	ctx = WithValue(ctx, true)
	if !IsUnattended(ctx) {
		t.Error("expected unattended after WithValue(true)")
	}
	ctx = WithValue(ctx, false)
	if IsUnattended(ctx) {
		t.Error("expected not unattended after WithValue(false)")
	}
}

func TestIsUnattended_Env(t *testing.T) {
	tests := []struct {
		name string
		env  string
		val  string
		want bool
	}{
		{"YCODE_UNATTENDED=1", "YCODE_UNATTENDED", "1", true},
		{"YCODE_UNATTENDED=true", "YCODE_UNATTENDED", "true", true},
		{"YCODE_UNATTENDED=yes", "YCODE_UNATTENDED", "yes", true},
		{"YCODE_UNATTENDED=0", "YCODE_UNATTENDED", "0", false},
		{"WEAVE_ID set", "WEAVE_ID", "weave-123", true},
		{"WEAVE_WORKSPACE set", "WEAVE_WORKSPACE", "ws-123", true},
		{"CI=true", "CI", "true", true},
		{"CI=false", "CI", "false", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("YCODE_UNATTENDED", "")
			t.Setenv("WEAVE_ID", "")
			t.Setenv("WEAVE_WORKSPACE", "")
			t.Setenv(tt.env, tt.val)
			ctx := context.Background()
			if got := IsUnattended(ctx); got != tt.want {
				t.Errorf("IsUnattended() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUnattended_EnvCleared(t *testing.T) {
	t.Setenv("YCODE_UNATTENDED", "")
	t.Setenv("WEAVE_ID", "")
	t.Setenv("WEAVE_WORKSPACE", "")
	if IsUnattended(context.Background()) {
		t.Error("expected not unattended when no env or context value set")
	}
}
