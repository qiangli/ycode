package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/git"
)

// maxDiffOutput is the maximum diff output size (50 KB).
const maxDiffOutput = 50 * 1024

// RegisterGitDiffHandler registers the view_diff tool handler.
func RegisterGitDiffHandler(r *Registry, workDir string, ge *git.GitExec) {
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
			return viewDiffMergeBase(ctx, ge, workDir, params.BaseBranch, params.Path)
		}

		return viewDiff(ctx, ge, workDir, params.Staged, params.Path, params.CommitRange)
	}
}

// viewDiffMergeBase shows the diff from the merge base of the current branch
// against a base branch (defaults to main/master).
func viewDiffMergeBase(ctx context.Context, ge *git.GitExec, workDir, baseBranch, path string) (string, error) {
	if baseBranch == "" {
		baseBranch = detectBaseBranch(ctx, ge, workDir)
	}

	// Find merge base.
	base, err := ge.RunOutput(ctx, workDir, "merge-base", baseBranch, "HEAD")
	if err != nil {
		return "", fmt.Errorf("git merge-base: %w", err)
	}

	// Run diff from merge base.
	args := []string{"diff", base + "..HEAD"}
	if path != "" {
		args = append(args, "--", path)
	}

	out, err := ge.Run(ctx, workDir, args...)
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}

	result := strings.TrimRight(out, "\n")
	if result == "" {
		return fmt.Sprintf("(no differences from merge base with %s)", baseBranch), nil
	}
	if len(result) > maxDiffOutput {
		result = result[:maxDiffOutput] + "\n... (truncated at 50KB)"
	}
	return result, nil
}

// detectBaseBranch tries to find main or master branch.
func detectBaseBranch(ctx context.Context, ge *git.GitExec, workDir string) string {
	for _, name := range []string{"main", "master"} {
		if err := ge.RunCheck(ctx, workDir, "rev-parse", "--verify", name); err == nil {
			return name
		}
	}
	return "main"
}

// viewDiff runs git diff with the specified options and returns the output.
func viewDiff(ctx context.Context, ge *git.GitExec, workDir string, staged bool, path, commitRange string) (string, error) {
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

	out, err := ge.Run(ctx, workDir, args...)
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}

	result := strings.TrimRight(out, "\n")
	if result == "" {
		return "(no differences)", nil
	}

	if len(result) > maxDiffOutput {
		result = result[:maxDiffOutput] + "\n... (truncated at 50KB)"
	}

	return result, nil
}
