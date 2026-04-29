// Package github provides GitHub API integration for ycode.
// It wraps go-github to provide PR, issue, and CI check operations
// as tool-registry-compatible functions.
package github

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var (
	httpsRemoteRE = regexp.MustCompile(`github\.com[:/]([^/]+)/([^/.]+?)(?:\.git)?$`)
	sshRemoteRE   = regexp.MustCompile(`git@github\.com:([^/]+)/([^/.]+?)(?:\.git)?$`)
)

// DetectRepo parses the git remote URL to extract the GitHub owner and repo.
// It checks the "origin" remote first.
func DetectRepo(gitDir string) (owner, repo string, err error) {
	cmd := exec.Command("git", "-C", gitDir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("git remote get-url origin: %w", err)
	}

	url := strings.TrimSpace(string(out))
	return ParseRemoteURL(url)
}

// ParseRemoteURL extracts owner/repo from a GitHub remote URL.
// Supports HTTPS (https://github.com/owner/repo.git) and
// SSH (git@github.com:owner/repo.git) formats.
func ParseRemoteURL(url string) (owner, repo string, err error) {
	if m := httpsRemoteRE.FindStringSubmatch(url); len(m) == 3 {
		return m[1], m[2], nil
	}
	if m := sshRemoteRE.FindStringSubmatch(url); len(m) == 3 {
		return m[1], m[2], nil
	}
	return "", "", fmt.Errorf("cannot parse GitHub remote URL: %q", url)
}
