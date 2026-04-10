package git

import (
	"fmt"
	"strings"
	"time"
)

// StaleBranch represents a branch that may be outdated.
type StaleBranch struct {
	Name       string
	LastCommit time.Time
	Age        time.Duration
	Author     string
}

// StaleBase detects if the current branch's base (e.g., main) has moved forward.
type StaleBase struct {
	BaseBranch     string
	CurrentBranch  string
	CommitsBehind  int
	MergeBaseAge   time.Duration
}

// DetectStaleBase checks if the current branch needs rebasing on its base.
func DetectStaleBase(dir string) (*StaleBase, error) {
	currentBranch, err := runGitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("get branch: %w", err)
	}
	currentBranch = strings.TrimSpace(currentBranch)

	baseBranch := detectMainBranch(dir)
	if currentBranch == baseBranch {
		return nil, nil // on base branch, not stale
	}

	// Find merge base.
	mergeBase, err := runGitOutput(dir, "merge-base", currentBranch, baseBranch)
	if err != nil {
		return nil, nil
	}
	mergeBase = strings.TrimSpace(mergeBase)

	// Count commits on base since merge point.
	countStr, err := runGitOutput(dir, "rev-list", "--count", mergeBase+".."+baseBranch)
	if err != nil {
		return nil, nil
	}

	count := 0
	fmt.Sscanf(strings.TrimSpace(countStr), "%d", &count)

	if count == 0 {
		return nil, nil // up to date
	}

	// Get merge base date.
	dateStr, err := runGitOutput(dir, "log", "-1", "--format=%ci", mergeBase)
	if err != nil {
		return nil, nil
	}

	mergeDate, err := time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(dateStr))
	if err != nil {
		return nil, nil
	}

	return &StaleBase{
		BaseBranch:    baseBranch,
		CurrentBranch: currentBranch,
		CommitsBehind: count,
		MergeBaseAge:  time.Since(mergeDate),
	}, nil
}

// DetectStaleBranches finds branches that haven't had commits recently.
func DetectStaleBranches(dir string, maxAge time.Duration) ([]StaleBranch, error) {
	output, err := runGitOutput(dir, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname:short)|%(committerdate:iso)|%(authorname)", "refs/heads/")
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}

	var stale []StaleBranch
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}

		name := parts[0]
		dateStr := parts[1]
		author := parts[2]

		commitDate, err := time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(dateStr))
		if err != nil {
			continue
		}

		age := time.Since(commitDate)
		if age > maxAge {
			stale = append(stale, StaleBranch{
				Name:       name,
				LastCommit: commitDate,
				Age:        age,
				Author:     author,
			})
		}
	}

	return stale, nil
}
