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
			MergeBase   bool   `json:"merge_base"`
			BaseBranch  string `json:"base_branch"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse view_diff input: %w", err)
		}

		// If merge_base is requested, compute diff from merge base.
		if params.MergeBase {
			return viewDiffMergeBase(ctx, workDir, params.BaseBranch, params.Path)
		}

		return viewDiff(ctx, workDir, params.Staged, params.Path, params.CommitRange)
	}
}

// viewDiffMergeBase shows the diff from the merge base of the current branch
// against a base branch (defaults to main/master).
func viewDiffMergeBase(ctx context.Context, workDir, baseBranch, path string) (string, error) {
	if baseBranch == "" {
		baseBranch = detectBaseBranch(workDir)
	}

	// Find merge base.
	mergeBase := exec.CommandContext(ctx, "git", "merge-base", baseBranch, "HEAD")
	mergeBase.Dir = workDir
	mbOut, err := mergeBase.Output()
	if err != nil {
		return "", fmt.Errorf("git merge-base: %w", err)
	}
	base := strings.TrimSpace(string(mbOut))

	// Run diff from merge base.
	args := []string{"diff", base + "..HEAD"}
	if path != "" {
		args = append(args, "--", path)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) == 0 {
		return "", fmt.Errorf("git diff: %w", err)
	}

	result := string(output)
	if result == "" {
		return fmt.Sprintf("(no differences from merge base with %s)", baseBranch), nil
	}
	if len(result) > maxDiffOutput {
		result = result[:maxDiffOutput] + "\n... (truncated at 50KB)"
	}
	return strings.TrimRight(result, "\n"), nil
}

// detectBaseBranch tries to find main or master branch.
func detectBaseBranch(workDir string) string {
	for _, name := range []string{"main", "master"} {
		cmd := exec.Command("git", "rev-parse", "--verify", name)
		cmd.Dir = workDir
		if err := cmd.Run(); err == nil {
			return name
		}
	}
	return "main"
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
