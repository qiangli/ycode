package github

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	ghlib "github.com/google/go-github/v84/github"
	"gopkg.in/yaml.v3"
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

	// 3. Read gh CLI config file directly (no external binary needed).
	if token := readGHConfigToken(); token != "" {
		return token
	}

	return ""
}

// readGHConfigToken reads the GitHub auth token from the gh CLI config file
// at ~/.config/gh/hosts.yml (or $GH_CONFIG_DIR/hosts.yml).
func readGHConfigToken() string {
	configDir := os.Getenv("GH_CONFIG_DIR")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDir = filepath.Join(home, ".config", "gh")
	}

	data, err := os.ReadFile(filepath.Join(configDir, "hosts.yml"))
	if err != nil {
		return ""
	}

	// hosts.yml structure: map of hostname → {oauth_token, user, ...}
	var hosts map[string]struct {
		OAuthToken string `yaml:"oauth_token"`
	}
	if err := yaml.Unmarshal(data, &hosts); err != nil {
		return ""
	}

	if host, ok := hosts["github.com"]; ok {
		return strings.TrimSpace(host.OAuthToken)
	}
	return ""
}
