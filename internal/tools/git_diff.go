package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// maxDiffOutput is the maximum diff output size (50 KB).
const maxDiffOutput = 50 * 1024

// RegisterGitDiffHandler registers the view_diff tool handler.
func RegisterGitDiffHandler(r *Registry, workDir string) {
	spec, ok := r.Get("view_diff")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Staged      bool   `json:"staged"`
			Path        string `json:"path"`
			CommitRange string `json:"commit_range"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse view_diff input: %w", err)
		}
		return viewDiff(ctx, workDir, params.Staged, params.Path, params.CommitRange)
	}
}

// viewDiff runs git diff with the specified options and returns the output.
func viewDiff(ctx context.Context, workDir string, staged bool, path, commitRange string) (string, error) {
	args := []string{"diff"}

	if staged {
		args = append(args, "--cached")
	}
	if commitRange != "" {
		args = append(args, commitRange)
	}
	if path != "" {
		args = append(args, "--", path)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		// git diff can return exit code 1 when there are differences.
		// Only treat it as an error if there's no output.
		if len(output) == 0 {
			return "", fmt.Errorf("git diff: %w", err)
		}
	}

	result := string(output)
	if result == "" {
		return "(no differences)", nil
	}

	// Truncate if output exceeds limit.
	if len(result) > maxDiffOutput {
		result = result[:maxDiffOutput] + "\n... (truncated at 50KB)"
	}

	return strings.TrimRight(result, "\n"), nil
}
