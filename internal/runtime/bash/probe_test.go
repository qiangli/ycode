package bash

import (
	"strings"
	"testing"
)

func TestSuggestAlternatives(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		wantCmd  string
		wantHint string
	}{
		{
			name:     "python not found",
			stderr:   "bash: python: command not found",
			wantCmd:  "python",
			wantHint: "python3",
		},
		{
			name:     "pip not found",
			stderr:   "bash: pip: command not found",
			wantCmd:  "pip",
			wantHint: "pip3",
		},
		{
			name:     "node not found",
			stderr:   "sh: node: not found",
			wantCmd:  "node",
			wantHint: "nodejs",
		},
		{
			name:     "docker not found",
			stderr:   "bash: docker: command not found",
			wantCmd:  "docker",
			wantHint: "podman",
		},
		{
			name:     "gcc not found",
			stderr:   "bash: gcc: command not found",
			wantCmd:  "gcc",
			wantHint: "clang",
		},
		{
			name:     "unknown command",
			stderr:   "bash: myweirdtool: command not found",
			wantCmd:  "myweirdtool",
			wantHint: "not found",
		},
		{
			name:     "no match",
			stderr:   "permission denied",
			wantHint: "",
		},
		{
			name:     "with line number",
			stderr:   "bash: line 1: python: command not found",
			wantCmd:  "python",
			wantHint: "python3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SuggestAlternatives(tt.stderr)
			if tt.wantHint == "" {
				if got != "" {
					t.Errorf("expected no hint, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantHint) {
				t.Errorf("expected hint containing %q, got %q", tt.wantHint, got)
			}
		})
	}
}
