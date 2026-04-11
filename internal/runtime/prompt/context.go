package prompt

// ProjectContext holds metadata about the current project.
type ProjectContext struct {
	WorkDir       string        `json:"work_dir"`
	CurrentDate   string        `json:"current_date,omitempty"`
	IsGitRepo     bool          `json:"is_git_repo"`
	GitBranch     string        `json:"git_branch,omitempty"`
	MainBranch    string        `json:"main_branch,omitempty"`
	GitUser       string        `json:"git_user,omitempty"`
	RecentCommits []string      `json:"recent_commits,omitempty"`
	GitStatus     string        `json:"git_status,omitempty"`
	GitDiff       string        `json:"git_diff,omitempty"`
	StagedFiles   []string      `json:"staged_files,omitempty"`
	Platform      string        `json:"platform"`
	Shell         string        `json:"shell"`
	OSVersion     string        `json:"os_version,omitempty"`
	Model         string        `json:"model,omitempty"`
	ContextFiles  []ContextFile `json:"context_files,omitempty"`
}

// ContextFile is a discovered instruction file (e.g., CLAUDE.md).
type ContextFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"` // for dedup
}
