package ralph

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/git"
)

// NewBashCheckFunc creates a CheckFunc that runs a shell command and reports
// pass/fail based on exit code.
func NewBashCheckFunc(command string) CheckFunc {
	return func(ctx context.Context) (bool, string, error) {
		out, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
		output := string(out)
		if err != nil {
			// Command failed — return output for diagnostics but no error
			// (the check itself ran successfully, the code under test failed).
			return false, output, nil
		}
		return true, output, nil
	}
}

// NewGitCommitFunc creates a CommitFunc that stages changed files by name and commits.
// It avoids "git add -A" per project conventions.
func NewGitCommitFunc(workDir string) CommitFunc {
	ge := git.NewGitExec(nil)
	return NewGitCommitFuncWith(workDir, ge)
}

// NewGitCommitFuncWith creates a CommitFunc using the provided GitExec.
func NewGitCommitFuncWith(workDir string, ge *git.GitExec) CommitFunc {
	return func(ctx context.Context, message string) error {
		// Get list of modified/added files (not untracked).
		out, err := ge.Run(ctx, workDir, "diff", "--name-only")
		if err != nil {
			return fmt.Errorf("git diff: %s", out)
		}
		files := splitNonEmpty(out)

		// Also get staged files.
		stagedOut, err := ge.Run(ctx, workDir, "diff", "--cached", "--name-only")
		if err != nil {
			return fmt.Errorf("git diff --cached: %s", stagedOut)
		}
		files = append(files, splitNonEmpty(stagedOut)...)

		// Get untracked files.
		untrackedOut, err := ge.Run(ctx, workDir, "ls-files", "--others", "--exclude-standard")
		if err != nil {
			return fmt.Errorf("git ls-files: %s", untrackedOut)
		}
		files = append(files, splitNonEmpty(untrackedOut)...)

		if len(files) == 0 {
			return nil // Nothing to commit.
		}

		// Deduplicate.
		seen := make(map[string]bool)
		var unique []string
		for _, f := range files {
			if !seen[f] {
				seen[f] = true
				unique = append(unique, f)
			}
		}

		// Stage files by name.
		args := append([]string{"add", "--"}, unique...)
		if _, err = ge.Run(ctx, workDir, args...); err != nil {
			return fmt.Errorf("git add: %w", err)
		}

		// Commit.
		if _, err = ge.Run(ctx, workDir, "commit", "-m", message); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}

		return nil
	}
}

// splitNonEmpty splits a string by newlines and returns non-empty trimmed lines.
func splitNonEmpty(s string) []string {
	var result []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
