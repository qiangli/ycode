package ralph

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
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
	return func(ctx context.Context, message string) error {
		// Get list of modified/added files (not untracked).
		out, err := exec.CommandContext(ctx, "git", "-C", workDir, "diff", "--name-only").CombinedOutput()
		if err != nil {
			return fmt.Errorf("git diff: %s", out)
		}
		files := splitNonEmpty(string(out))

		// Also get staged files.
		stagedOut, err := exec.CommandContext(ctx, "git", "-C", workDir, "diff", "--cached", "--name-only").CombinedOutput()
		if err != nil {
			return fmt.Errorf("git diff --cached: %s", stagedOut)
		}
		files = append(files, splitNonEmpty(string(stagedOut))...)

		// Get untracked files.
		untrackedOut, err := exec.CommandContext(ctx, "git", "-C", workDir, "ls-files", "--others", "--exclude-standard").CombinedOutput()
		if err != nil {
			return fmt.Errorf("git ls-files: %s", untrackedOut)
		}
		files = append(files, splitNonEmpty(string(untrackedOut))...)

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
		args := append([]string{"-C", workDir, "add", "--"}, unique...)
		out, err = exec.CommandContext(ctx, "git", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("git add: %s", out)
		}

		// Commit.
		out, err = exec.CommandContext(ctx, "git", "-C", workDir, "commit", "-m", message).CombinedOutput()
		if err != nil {
			return fmt.Errorf("git commit: %s", out)
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
