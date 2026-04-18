package builtin

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// maxDiffBytes caps the diff content sent to the LLM (~3000 tokens).
	maxDiffBytes = 12_000

	// commitMaxTokens limits the LLM output for commit message generation.
	commitMaxTokens = 256

	commitSystemPrompt = `Generate a concise git commit message for the given diff.

Rules:
- Use conventional commit format: <type>: <short summary>
- Types: fix, feat, docs, refactor, test, chore, perf
- Match the style of the recent commits shown
- Focus on WHY the change was made, not a mechanical list of what changed
- Keep the first line under 72 characters
- Add a blank line and a brief body only if the change needs explanation
- Output ONLY the commit message — no markdown fences, no commentary`
)

// CommitGenerator creates git commits with LLM-generated messages,
// bypassing the full conversation runtime.
type CommitGenerator struct {
	chain   *ModelChain
	workDir string
}

// NewCommitGenerator creates a CommitGenerator.
func NewCommitGenerator(chain *ModelChain, workDir string) *CommitGenerator {
	return &CommitGenerator{chain: chain, workDir: workDir}
}

// CommitRequest controls what to commit and how.
type CommitRequest struct {
	// FilesToStage lists specific files to stage. If empty, uses whatever
	// is already staged; if nothing is staged, stages all modified files.
	FilesToStage []string

	// DryRun generates the message without running git add/commit.
	DryRun bool

	// Hint provides additional context for the LLM (e.g., "fixes login bug").
	Hint string
}

// CommitResult holds the outcome of a commit operation.
type CommitResult struct {
	Message   string   // generated commit message
	Hash      string   // short commit hash (empty if DryRun)
	Staged    []string // files that were staged
	Remaining []string // files still uncommitted after the commit
	HookError string   // non-empty if pre-commit hook failed
}

// Generate runs the full commit workflow: gather git context, generate a
// commit message via a single LLM call, stage files, and commit.
func (cg *CommitGenerator) Generate(ctx context.Context, req *CommitRequest) (*CommitResult, error) {
	if req == nil {
		req = &CommitRequest{}
	}

	// Step 1: Gather git context.
	gc, err := cg.gatherContext()
	if err != nil {
		return nil, fmt.Errorf("gather git context: %w", err)
	}

	if gc.diff == "" && gc.stat == "" && len(gc.stagedFiles) == 0 && len(gc.modifiedFiles) == 0 {
		return nil, fmt.Errorf("no changes to commit")
	}

	// Step 2: Stage files if requested.
	staged := req.FilesToStage
	if len(staged) > 0 && !req.DryRun {
		if err := cg.stageFiles(staged); err != nil {
			return nil, fmt.Errorf("stage files: %w", err)
		}
		// Re-read staged diff after staging.
		if out, err := cg.git("diff", "--cached"); err == nil && strings.TrimSpace(out) != "" {
			gc.diff = strings.TrimSpace(out)
		}
		if out, err := cg.git("diff", "--cached", "--stat"); err == nil {
			gc.stat = strings.TrimSpace(out)
		}
	}

	// Step 3: Generate commit message.
	message, err := cg.generateMessage(ctx, gc, req.Hint)
	if err != nil {
		slog.Warn("LLM commit message generation failed, using template fallback", "error", err)
		message = cg.templateFallback(gc)
	}

	result := &CommitResult{
		Message: message,
		Staged:  gc.stagedFiles,
	}

	if req.DryRun {
		return result, nil
	}

	// Step 4: Stage remaining files if nothing was explicitly staged.
	if len(staged) == 0 && len(gc.stagedFiles) == 0 {
		// Nothing staged — stage all modified/added files.
		if err := cg.stageAll(gc.modifiedFiles); err != nil {
			return nil, fmt.Errorf("stage files: %w", err)
		}
		result.Staged = gc.modifiedFiles
	}

	// Step 5: Commit.
	commitOut, err := cg.git("commit", "-m", message)
	if err != nil {
		// Check for hook failure.
		result.HookError = strings.TrimSpace(commitOut)
		return result, fmt.Errorf("git commit failed: %w", err)
	}

	// Step 6: Get commit hash and remaining status.
	if hash, err := cg.git("rev-parse", "--short", "HEAD"); err == nil {
		result.Hash = strings.TrimSpace(hash)
	}
	if out, err := cg.git("status", "--porcelain"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line != "" {
				result.Remaining = append(result.Remaining, line)
			}
		}
	}

	return result, nil
}

// gitContext holds the gathered git state.
type gitContext struct {
	recentLog     string   // git log --oneline -5
	diff          string   // full diff (staged or unstaged)
	stat          string   // diff --stat output
	stagedFiles   []string // files already staged
	modifiedFiles []string // all modified/untracked files
}

// gatherContext runs git commands to collect diff, stat, log, and file lists.
func (cg *CommitGenerator) gatherContext() (*gitContext, error) {
	gc := &gitContext{}

	// Recent commit log for style matching.
	if out, err := cg.git("log", "--oneline", "-5"); err == nil {
		gc.recentLog = strings.TrimSpace(out)
	}

	// Check for staged changes first.
	if out, err := cg.git("diff", "--cached", "--name-only"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line != "" {
				gc.stagedFiles = append(gc.stagedFiles, line)
			}
		}
	}

	if len(gc.stagedFiles) > 0 {
		// Use staged diff.
		if out, err := cg.git("diff", "--cached"); err == nil {
			gc.diff = strings.TrimSpace(out)
		}
		if out, err := cg.git("diff", "--cached", "--stat"); err == nil {
			gc.stat = strings.TrimSpace(out)
		}
	} else {
		// Nothing staged — use unstaged diff.
		if out, err := cg.git("diff"); err == nil {
			gc.diff = strings.TrimSpace(out)
		}
		if out, err := cg.git("diff", "--stat"); err == nil {
			gc.stat = strings.TrimSpace(out)
		}
	}

	// All modified/untracked files from porcelain status.
	if out, err := cg.git("status", "--porcelain"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if len(line) > 3 {
				gc.modifiedFiles = append(gc.modifiedFiles, strings.TrimSpace(line[3:]))
			}
		}
	}

	return gc, nil
}

// generateMessage builds the LLM prompt and makes one API call.
func (cg *CommitGenerator) generateMessage(ctx context.Context, gc *gitContext, hint string) (string, error) {
	var userContent strings.Builder

	if gc.recentLog != "" {
		userContent.WriteString("Recent commits (for style reference):\n")
		userContent.WriteString(gc.recentLog)
		userContent.WriteString("\n\n")
	}

	if gc.stat != "" {
		userContent.WriteString("Changed files:\n")
		userContent.WriteString(gc.stat)
		userContent.WriteString("\n\n")
	}

	userContent.WriteString("Diff:\n")
	userContent.WriteString(truncateDiff(gc.diff))

	if hint != "" {
		userContent.WriteString("\n\nAdditional context: ")
		userContent.WriteString(hint)
	}

	raw, err := cg.chain.SingleShot(ctx, commitSystemPrompt, userContent.String(), commitMaxTokens)
	if err != nil {
		return "", err
	}

	return cleanCommitMessage(raw), nil
}

// templateFallback generates a commit message from the stat output without LLM.
func (cg *CommitGenerator) templateFallback(gc *gitContext) string {
	fileCount := len(gc.stagedFiles)
	if fileCount == 0 {
		fileCount = len(gc.modifiedFiles)
	}
	if fileCount == 0 {
		fileCount = 1
	}

	// Infer type from file paths.
	commitType := inferCommitType(gc.stagedFiles, gc.modifiedFiles)

	if fileCount == 1 {
		files := gc.stagedFiles
		if len(files) == 0 {
			files = gc.modifiedFiles
		}
		if len(files) > 0 {
			return fmt.Sprintf("%s: update %s", commitType, filepath.Base(files[0]))
		}
	}

	return fmt.Sprintf("%s: update %d files", commitType, fileCount)
}

// inferCommitType guesses the conventional commit type from file paths.
func inferCommitType(staged, modified []string) string {
	files := staged
	if len(files) == 0 {
		files = modified
	}

	var hasTest, hasDocs, hasCode bool
	for _, f := range files {
		lower := strings.ToLower(f)
		switch {
		case strings.Contains(lower, "_test.go") || strings.Contains(lower, "test_") ||
			strings.HasPrefix(lower, "test") || strings.Contains(lower, "/test/"):
			hasTest = true
		case strings.HasSuffix(lower, ".md") || strings.Contains(lower, "/docs/") ||
			strings.Contains(lower, "readme") || strings.Contains(lower, "changelog"):
			hasDocs = true
		default:
			hasCode = true
		}
	}

	switch {
	case hasTest && !hasCode && !hasDocs:
		return "test"
	case hasDocs && !hasCode && !hasTest:
		return "docs"
	default:
		return "chore"
	}
}

// truncateDiff limits diff size to maxDiffBytes.
func truncateDiff(diff string) string {
	if len(diff) <= maxDiffBytes {
		return diff
	}

	// Find a clean line boundary near the limit.
	cut := maxDiffBytes
	if idx := strings.LastIndex(diff[:cut], "\n"); idx > 0 {
		cut = idx + 1
	}

	totalLines := strings.Count(diff, "\n")
	shownLines := strings.Count(diff[:cut], "\n")

	return diff[:cut] + fmt.Sprintf("\n[diff truncated: showing %d of %d lines, %d of %d bytes]",
		shownLines, totalLines, cut, len(diff))
}

// cleanCommitMessage strips markdown fences and leading/trailing whitespace.
func cleanCommitMessage(raw string) string {
	msg := strings.TrimSpace(raw)

	// Strip markdown code fences.
	if strings.HasPrefix(msg, "```") {
		lines := strings.Split(msg, "\n")
		// Remove first and last line if they're fences.
		if len(lines) >= 2 {
			start := 1
			end := len(lines)
			if strings.HasPrefix(lines[end-1], "```") {
				end--
			}
			msg = strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		}
	}

	// Strip surrounding quotes.
	if len(msg) >= 2 && msg[0] == '"' && msg[len(msg)-1] == '"' {
		msg = msg[1 : len(msg)-1]
	}

	return msg
}

// stageFiles runs git add on specific files.
func (cg *CommitGenerator) stageFiles(files []string) error {
	args := append([]string{"add", "--"}, files...)
	_, err := cg.git(args...)
	return err
}

// stageAll stages a list of modified files.
func (cg *CommitGenerator) stageAll(files []string) error {
	if len(files) == 0 {
		return nil
	}
	return cg.stageFiles(files)
}

// git runs a git command in the working directory and returns combined output.
func (cg *CommitGenerator) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cg.workDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// FormatResult produces a human-readable summary of the commit result.
func FormatResult(r *CommitResult) string {
	var b strings.Builder

	if r.HookError != "" {
		fmt.Fprintf(&b, "Commit failed (pre-commit hook):\n%s\n", r.HookError)
		return b.String()
	}

	if r.Hash != "" {
		fmt.Fprintf(&b, "Committed: %s\n", r.Hash)
	}
	fmt.Fprintf(&b, "Message: %s\n", firstLine(r.Message))

	if len(r.Staged) > 0 {
		fmt.Fprintf(&b, "Staged: %s\n", strings.Join(r.Staged, ", "))
	}

	if len(r.Remaining) > 0 {
		fmt.Fprintf(&b, "\nRemaining uncommitted:\n")
		for _, f := range r.Remaining {
			fmt.Fprintf(&b, "  %s\n", f)
		}
	}

	return b.String()
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return first
}
