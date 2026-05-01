package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/git"
)

// maxGitOutput is the maximum output size for git commands (50 KB).
const maxGitOutput = 50 * 1024

// GitToolsDeps holds dependencies for git tool handlers.
type GitToolsDeps struct {
	WorkDir string
	GitExec *git.GitExec
}

// RegisterGitHandlers registers all git operation tool handlers.
func RegisterGitHandlers(r *Registry, deps *GitToolsDeps) {
	workDir := ""
	var ge *git.GitExec
	if deps != nil {
		workDir = deps.WorkDir
		ge = deps.GitExec
	}
	if ge == nil {
		ge = git.NewGitExec(nil) // direct exec fallback
	}

	RegisterGitDiffHandler(r, workDir, ge)
	registerGitStatusHandler(r, workDir, ge)
	registerGitLogHandler(r, workDir, ge)
	registerGitCommitHandler(r, workDir, ge)
	registerGitBranchHandler(r, workDir, ge)
	registerGitStashHandler(r, workDir, ge)
	registerGitAddHandler(r, workDir, ge)
	registerGitResetHandler(r, workDir, ge)
	registerGitShowHandler(r, workDir, ge)
}

func registerGitStatusHandler(r *Registry, workDir string, ge *git.GitExec) {
	spec, ok := r.Get("git_status")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		out, err := ge.Run(ctx, workDir, "--no-optional-locks", "status", "--short", "--branch")
		if err != nil {
			return "", fmt.Errorf("git status: %w", err)
		}
		result := strings.TrimRight(out, "\n")
		if result == "" {
			return "(clean working tree)", nil
		}
		return truncateOutput(result), nil
	}
}

func registerGitLogHandler(r *Registry, workDir string, ge *git.GitExec) {
	spec, ok := r.Get("git_log")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Count   int    `json:"count"`
			Oneline bool   `json:"oneline"`
			Path    string `json:"path"`
			Author  string `json:"author"`
			Since   string `json:"since"`
			Until   string `json:"until"`
			Diff    string `json:"diff"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse git_log input: %w", err)
		}

		count := params.Count
		if count <= 0 {
			count = 10
		}

		args := []string{"log", "-" + strconv.Itoa(count)}

		if params.Oneline || params.Count == 0 {
			args = append(args, "--oneline")
		} else {
			args = append(args, "--format=%h %an %ad %s", "--date=short")
		}
		if params.Author != "" {
			args = append(args, "--author="+params.Author)
		}
		if params.Since != "" {
			args = append(args, "--since="+params.Since)
		}
		if params.Until != "" {
			args = append(args, "--until="+params.Until)
		}
		if params.Diff != "" {
			args = append(args, params.Diff)
		}
		if params.Path != "" {
			args = append(args, "--", params.Path)
		}

		out, err := ge.Run(ctx, workDir, args...)
		if err != nil {
			return "", fmt.Errorf("git log: %w", err)
		}
		result := strings.TrimRight(out, "\n")
		if result == "" {
			return "(no commits)", nil
		}
		return truncateOutput(result), nil
	}
}

func registerGitCommitHandler(r *Registry, workDir string, ge *git.GitExec) {
	spec, ok := r.Get("git_commit")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Message string   `json:"message"`
			Files   []string `json:"files"`
			All     bool     `json:"all"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse git_commit input: %w", err)
		}
		if params.Message == "" {
			return "", fmt.Errorf("commit message is required")
		}

		// Stage files if specified.
		if len(params.Files) > 0 {
			addArgs := append([]string{"add"}, params.Files...)
			if _, err := ge.Run(ctx, workDir, addArgs...); err != nil {
				return "", fmt.Errorf("git add: %w", err)
			}
		}

		// Build commit command.
		commitArgs := []string{"commit"}
		if params.All {
			commitArgs = append(commitArgs, "-a")
		}
		commitArgs = append(commitArgs, "-m", params.Message)

		out, err := ge.Run(ctx, workDir, commitArgs...)
		if err != nil {
			return "", fmt.Errorf("git commit: %w", err)
		}
		return strings.TrimRight(out, "\n"), nil
	}
}

func registerGitBranchHandler(r *Registry, workDir string, ge *git.GitExec) {
	spec, ok := r.Get("git_branch")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Action     string `json:"action"`
			Name       string `json:"name"`
			StartPoint string `json:"start_point"`
			Remote     bool   `json:"remote"`
			All        bool   `json:"all"`
			Contains   string `json:"contains"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse git_branch input: %w", err)
		}

		action := params.Action
		if action == "" {
			action = "list"
		}

		var args []string
		switch action {
		case "list":
			args = []string{"branch", "-v"}
			if params.All {
				args = append(args, "-a")
			} else if params.Remote {
				args = append(args, "-r")
			}
			if params.Contains != "" {
				args = append(args, "--contains", params.Contains)
			}
		case "create":
			if params.Name == "" {
				return "", fmt.Errorf("branch name is required for create")
			}
			args = []string{"branch", params.Name}
			if params.StartPoint != "" {
				args = append(args, params.StartPoint)
			}
		case "switch":
			if params.Name == "" {
				return "", fmt.Errorf("branch name is required for switch")
			}
			args = []string{"checkout", params.Name}
		case "delete":
			if params.Name == "" {
				return "", fmt.Errorf("branch name is required for delete")
			}
			args = []string{"branch", "-d", params.Name}
		default:
			return "", fmt.Errorf("unknown branch action: %s", action)
		}

		out, err := ge.Run(ctx, workDir, args...)
		if err != nil {
			return "", fmt.Errorf("git branch %s: %w", action, err)
		}
		result := strings.TrimRight(out, "\n")
		if result == "" && action == "create" {
			return fmt.Sprintf("Branch '%s' created", params.Name), nil
		}
		return result, nil
	}
}

func registerGitStashHandler(r *Registry, workDir string, ge *git.GitExec) {
	spec, ok := r.Get("git_stash")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Action  string `json:"action"`
			Message string `json:"message"`
			Index   int    `json:"index"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse git_stash input: %w", err)
		}

		action := params.Action
		if action == "" {
			action = "push"
		}

		var args []string
		switch action {
		case "push":
			args = []string{"stash", "push"}
			if params.Message != "" {
				args = append(args, "-m", params.Message)
			}
		case "pop":
			args = []string{"stash", "pop", fmt.Sprintf("stash@{%d}", params.Index)}
		case "list":
			args = []string{"stash", "list"}
		case "drop":
			args = []string{"stash", "drop", fmt.Sprintf("stash@{%d}", params.Index)}
		case "show":
			args = []string{"stash", "show", "-p", fmt.Sprintf("stash@{%d}", params.Index)}
		default:
			return "", fmt.Errorf("unknown stash action: %s", action)
		}

		out, err := ge.Run(ctx, workDir, args...)
		if err != nil {
			return "", fmt.Errorf("git stash %s: %w", action, err)
		}
		result := strings.TrimRight(out, "\n")
		if result == "" {
			return fmt.Sprintf("stash %s completed", action), nil
		}
		return truncateOutput(result), nil
	}
}

func registerGitAddHandler(r *Registry, workDir string, ge *git.GitExec) {
	spec, ok := r.Get("git_add")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Files []string `json:"files"`
			All   bool     `json:"all"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse git_add input: %w", err)
		}

		var args []string
		if params.All {
			args = []string{"add", "-A"}
		} else if len(params.Files) > 0 {
			args = append([]string{"add", "--"}, params.Files...)
		} else {
			return "", fmt.Errorf("either 'files' or 'all' must be specified")
		}

		out, err := ge.Run(ctx, workDir, args...)
		if err != nil {
			return "", fmt.Errorf("git add: %w", err)
		}
		result := strings.TrimRight(out, "\n")
		if result == "" {
			return "files staged successfully", nil
		}
		return result, nil
	}
}

func registerGitResetHandler(r *Registry, workDir string, ge *git.GitExec) {
	spec, ok := r.Get("git_reset")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Files []string `json:"files"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse git_reset input: %w", err)
		}

		var args []string
		if len(params.Files) > 0 {
			args = append([]string{"reset", "HEAD", "--"}, params.Files...)
		} else {
			args = []string{"reset", "HEAD"}
		}

		out, err := ge.Run(ctx, workDir, args...)
		if err != nil {
			return "", fmt.Errorf("git reset: %w", err)
		}
		result := strings.TrimRight(out, "\n")
		if result == "" {
			return "unstaged successfully", nil
		}
		return result, nil
	}
}

func registerGitShowHandler(r *Registry, workDir string, ge *git.GitExec) {
	spec, ok := r.Get("git_show")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Revision string `json:"revision"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse git_show input: %w", err)
		}
		if params.Revision == "" {
			return "", fmt.Errorf("revision is required")
		}

		args := []string{"show", params.Revision}

		out, err := ge.Run(ctx, workDir, args...)
		if err != nil {
			return "", fmt.Errorf("git show: %w", err)
		}
		return truncateOutput(strings.TrimRight(out, "\n")), nil
	}
}

func truncateOutput(s string) string {
	if len(s) > maxGitOutput {
		return s[:maxGitOutput] + "\n... (truncated at 50KB)"
	}
	return s
}
