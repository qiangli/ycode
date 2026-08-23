package bash

import "testing"

func TestCommandMakesProgress(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		// Pure exploration — varied read/search shapes, the incident pattern.
		{"grep -rn 'IterationBudget' internal/", false},
		{"cat internal/cli/app.go", false},
		{"ls -la cmd/", false},
		{"find . -name '*.go' -newer go.mod", false},
		{"head -50 README.md | tail -20", false},
		{"git log --oneline -10", false},
		{"git status", false},
		{"git diff HEAD~1", false},

		// Writes and mutations.
		{"rm -rf build/", true},
		{"mkdir -p out && cp a.txt out/", true},
		{"git commit -m 'fix'", true},
		{"echo done > result.txt", true},

		// Build/test runners: measurable progress even though the safety
		// classifier defaults them to read-only.
		{"go test ./internal/...", true},
		{"go build ./cmd/ycode/", true},
		{"make check", true},
		{"cargo test", true},
		{"npm run lint", true},
		{"pytest -x tests/", true},
		{"bashy dag build", true},

		// A runner buried behind a read-only pipeline stage still counts.
		{"git status && go test ./...", true},
	}
	for _, tt := range tests {
		if got := CommandMakesProgress(tt.command); got != tt.want {
			t.Errorf("CommandMakesProgress(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}
