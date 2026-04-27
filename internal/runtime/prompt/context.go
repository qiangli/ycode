package prompt

import "github.com/qiangli/ycode/internal/runtime/memory"

// DegradedTool describes a tool with a success rate below the quality threshold.
type DegradedTool struct {
	Name         string  `json:"name"`
	SuccessRate  float64 `json:"success_rate"`
	TotalCalls   int     `json:"total_calls"`
	FailureCount int     `json:"failure_count"`
}

// DiagnosticsInfo holds runtime diagnostics surfaced to the system prompt.
type DiagnosticsInfo struct {
	// DegradedTools lists tools whose success rate is below threshold.
	DegradedTools []DegradedTool `json:"degraded_tools,omitempty"`
	// ContextHealthPct is the context usage as a percentage (0-100+).
	ContextHealthPct int `json:"context_health_pct,omitempty"`
	// ContextHealthLevel is "healthy", "warning", "critical", or "overflow".
	ContextHealthLevel string `json:"context_health_level,omitempty"`
	// PriorSessionSummary is a compact summary from the previous session's
	// ghost snapshot, injected on the first turn of a resumed session.
	PriorSessionSummary string `json:"prior_session_summary,omitempty"`
}

// ProjectContext holds metadata about the current project.
type ProjectContext struct {
	WorkDir       string           `json:"work_dir"`
	ProjectRoot   string           `json:"project_root,omitempty"` // git root or WorkDir; upper bound for JIT discovery
	CurrentDate   string           `json:"current_date,omitempty"`
	IsGitRepo     bool             `json:"is_git_repo"`
	GitBranch     string           `json:"git_branch,omitempty"`
	MainBranch    string           `json:"main_branch,omitempty"`
	GitUser       string           `json:"git_user,omitempty"`
	RecentCommits []string         `json:"recent_commits,omitempty"`
	GitStatus     string           `json:"git_status,omitempty"`
	GitDiff       string           `json:"git_diff,omitempty"`
	StagedFiles   []string         `json:"staged_files,omitempty"`
	Platform      string           `json:"platform"`
	Shell         string           `json:"shell"`
	OSVersion     string           `json:"os_version,omitempty"`
	Model         string           `json:"model,omitempty"`
	ContextFiles  []ContextFile    `json:"context_files,omitempty"`
	AllowedDirs   []string         `json:"allowed_dirs,omitempty"`
	ActiveTopic   string           `json:"active_topic,omitempty"` // current high-level task focus
	Personality   string           `json:"personality,omitempty"`  // builtin personality name (e.g., "pirate", "stern")
	Memories      []*memory.Memory `json:"memories,omitempty"`
	Diagnostics   *DiagnosticsInfo `json:"diagnostics,omitempty"`   // runtime diagnostics for system prompt
	RepoMapText   string           `json:"repo_map_text,omitempty"` // pre-rendered repo map for system prompt
}

// ContextFile is a discovered instruction file (e.g., CLAUDE.md).
type ContextFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"` // for dedup
}
