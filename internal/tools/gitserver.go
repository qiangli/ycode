package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/gitserver"
)

// gitServerClient is the module-level Gitea API client, set via SetGitServer.
// Nil when the git server is not running.
var gitServerClient *gitserver.Client

// SetGitServer injects the Gitea API client for the git server tools.
func SetGitServer(c *gitserver.Client) {
	gitServerClient = c
}

// RegisterGitServerHandlers wires up the GitServer* tool handlers.
func RegisterGitServerHandlers(r *Registry) {
	if spec, ok := r.Get("GitServerRepoList"); ok {
		spec.Handler = handleGitServerRepoList
	}
	if spec, ok := r.Get("GitServerRepoCreate"); ok {
		spec.Handler = handleGitServerRepoCreate
	}
	if spec, ok := r.Get("GitServerWorktreeCreate"); ok {
		spec.Handler = handleGitServerWorktreeCreate
	}
	if spec, ok := r.Get("GitServerWorktreeMerge"); ok {
		spec.Handler = handleGitServerWorktreeMerge
	}
	if spec, ok := r.Get("GitServerWorktreeCleanup"); ok {
		spec.Handler = handleGitServerWorktreeCleanup
	}
}

func checkGitServer() error {
	if gitServerClient == nil {
		return fmt.Errorf("Git server is not available. Start the server with `ycode serve` first.")
	}
	return nil
}

func handleGitServerRepoList(ctx context.Context, input json.RawMessage) (string, error) {
	if err := checkGitServer(); err != nil {
		return "", err
	}

	var params struct {
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse GitServerRepoList input: %w", err)
	}

	repos, err := gitServerClient.ListRepos(ctx)
	if err != nil {
		return "", fmt.Errorf("list repos: %w", err)
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > len(repos) {
		limit = len(repos)
	}
	repos = repos[:limit]

	if len(repos) == 0 {
		return "(no repositories)", nil
	}

	var lines []string
	for _, r := range repos {
		lines = append(lines, fmt.Sprintf("- %s  clone: %s  web: %s", r.FullName, r.CloneURL, r.HTMLURL))
	}
	return strings.Join(lines, "\n"), nil
}

func handleGitServerRepoCreate(ctx context.Context, input json.RawMessage) (string, error) {
	if err := checkGitServer(); err != nil {
		return "", err
	}

	var params struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse GitServerRepoCreate input: %w", err)
	}
	if params.Name == "" {
		return "", fmt.Errorf("name is required")
	}

	repo, err := gitServerClient.CreateRepo(ctx, params.Name, params.Description)
	if err != nil {
		return "", fmt.Errorf("create repo: %w", err)
	}

	return fmt.Sprintf("Created repository %s\nClone URL: %s\nWeb UI: %s", repo.FullName, repo.CloneURL, repo.HTMLURL), nil
}

func handleGitServerWorktreeCreate(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		RepoDir string `json:"repo_dir"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse GitServerWorktreeCreate input: %w", err)
	}
	if params.RepoDir == "" {
		return "", fmt.Errorf("repo_dir is required")
	}
	if params.AgentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}

	info, err := gitserver.PrepareWorkspace(ctx, params.RepoDir, params.AgentID, gitserver.WorkspaceWorktree)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Worktree created\nPath: %s\nBranch: %s\nRepo: %s", info.Path, info.Branch, info.RepoDir), nil
}

func handleGitServerWorktreeMerge(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		RepoDir      string `json:"repo_dir"`
		WorktreePath string `json:"worktree_path"`
		Branch       string `json:"branch"`
		BaseBranch   string `json:"base_branch"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse GitServerWorktreeMerge input: %w", err)
	}
	if params.RepoDir == "" || params.Branch == "" || params.BaseBranch == "" {
		return "", fmt.Errorf("repo_dir, branch, and base_branch are required")
	}

	info := &gitserver.WorkspaceInfo{
		Mode:    gitserver.WorkspaceWorktree,
		Path:    params.WorktreePath,
		Branch:  params.Branch,
		RepoDir: params.RepoDir,
	}

	if err := gitserver.MergeWorktree(ctx, info, params.BaseBranch); err != nil {
		return "", err
	}

	return fmt.Sprintf("Merged branch %s into %s", params.Branch, params.BaseBranch), nil
}

func handleGitServerWorktreeCleanup(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		RepoDir      string `json:"repo_dir"`
		WorktreePath string `json:"worktree_path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse GitServerWorktreeCleanup input: %w", err)
	}
	if params.RepoDir == "" || params.WorktreePath == "" {
		return "", fmt.Errorf("repo_dir and worktree_path are required")
	}

	info := &gitserver.WorkspaceInfo{
		Mode:    gitserver.WorkspaceWorktree,
		Path:    params.WorktreePath,
		RepoDir: params.RepoDir,
	}

	if err := gitserver.CleanupWorkspace(ctx, info); err != nil {
		return "", err
	}

	return fmt.Sprintf("Cleaned up worktree at %s", params.WorktreePath), nil
}
