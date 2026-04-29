package github

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"

	ghlib "github.com/google/go-github/v84/github"
)

// NewClient creates an authenticated GitHub client. It tries auth sources
// in order: GITHUB_TOKEN, GH_TOKEN, `gh auth token` CLI fallback.
// Returns nil if no auth is available.
func NewClient(ctx context.Context) *ghlib.Client {
	token := resolveToken()
	if token == "" {
		return nil
	}
	return ghlib.NewClient(nil).WithAuthToken(token)
}

// NewClientWithToken creates a GitHub client with the given token.
func NewClientWithToken(token string) *ghlib.Client {
	return ghlib.NewClient(nil).WithAuthToken(token)
}

// NewClientWithHTTPClient creates a GitHub client with a custom HTTP client.
// Used primarily for testing with httptest.Server.
func NewClientWithHTTPClient(httpClient *http.Client) *ghlib.Client {
	return ghlib.NewClient(httpClient)
}

// resolveToken tries multiple auth sources in priority order.
func resolveToken() string {
	// 1. GITHUB_TOKEN env var (highest priority).
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}

	// 2. GH_TOKEN env var (GitHub CLI compat).
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return token
	}

	// 3. GitHub CLI fallback.
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		token := strings.TrimSpace(string(out))
		if token != "" {
			return token
		}
	}

	return ""
}
