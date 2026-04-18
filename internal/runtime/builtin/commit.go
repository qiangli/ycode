package builtin

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const (
	// maxDiffBytes caps the diff content sent to the LLM.
	maxDiffBytes = 24_000

	// commitMaxTokens limits the LLM output for commit message generation.
	commitMaxTokens = 256

	// commitSystemPrompt is adapted from aider's proven prompt, which follows
	// the Conventional Commits specification (https://www.conventionalcommits.org/en/v1.0.0/).
	commitSystemPrompt = `You are an expert software engineer that generates concise, one-line Git commit messages based on the provided diffs.
Review the provided context and diffs which are about to be committed to a git repo.
Review the diffs carefully.
Generate a one-line commit message for those changes.
The commit message MUST be structured as: <type>[optional scope]: <description>
Use these for <type>: fix, feat, build, chore, ci, docs, style, refactor, perf, test
Optionally add a scope in parentheses when changes are localized, e.g. fix(api): handle nil response.

Ensure the commit message:
- Starts with the appropriate type prefix.
- Is in the imperative mood (e.g., "add feature" not "added feature" or "adding feature").
- Does not exceed 72 characters.
- Specifically names the feature, module, or component affected — never use vague descriptions like "update files" or "make changes".

Reply only with the one-line commit message, without any additional text, explanations, or line breaks.`
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

	// Context provides conversation history or other context about what
	// changes were made and why — passed to the LLM alongside the diff
	// (similar to aider's context parameter).
	Context string
}

// CommitResult holds the outcome of a commit operation.
type CommitResult struct {
	Message   string        // generated commit message
	Hash      string        // short commit hash (empty if DryRun)
	Staged    []string      // files that were staged
	Remaining []string      // files still uncommitted after the commit
	HookError string        // non-empty if pre-commit hook failed
	Duration  time.Duration // total wall-clock time for the operation
}

// Generate runs the full commit workflow:
//  1. Check for changes
//  2. Stage files (so git diff --cached works for untracked files too)
//  3. Gather diff from staged changes
//  4. Generate commit message via single LLM call
//  5. Commit
func (cg *CommitGenerator) Generate(ctx context.Context, req *CommitRequest) (*CommitResult, error) {
	if req == nil {
		req = &CommitRequest{}
	}

	start := time.Now()
	result := &CommitResult{}

	// Step 1: Check what needs committing.
	gc, err := cg.gatherPreStageContext()
	if err != nil {
		return nil, fmt.Errorf("gather git context: %w", err)
	}

	slog.Info("commit: pre-stage context",
		"staged", len(gc.stagedFiles),
		"modified", len(gc.modifiedFiles),
		"staged_files", gc.stagedFiles,
		"modified_files", gc.modifiedFiles,
	)

	if len(gc.stagedFiles) == 0 && len(gc.modifiedFiles) == 0 {
		return nil, fmt.Errorf("no changes to commit")
	}

	// Step 2: Stage files BEFORE reading the diff — this is critical because
	// git diff doesn't show untracked files, so we need them staged first.
	if !req.DryRun {
		if len(req.FilesToStage) > 0 {
			if err := cg.stageFiles(req.FilesToStage); err != nil {
				return nil, fmt.Errorf("stage files: %w", err)
			}
			result.Staged = req.FilesToStage
		} else if len(gc.stagedFiles) == 0 {
			// Nothing pre-staged and no explicit files — stage everything.
			if out, err := cg.git("add", "-A"); err != nil {
				return nil, fmt.Errorf("stage files: %s", strings.TrimSpace(out))
			}
			result.Staged = gc.modifiedFiles
		} else {
			result.Staged = gc.stagedFiles
		}
	}

	// Step 3: Now read the diff from staged changes — this captures
	// previously untracked files that are now staged.
	gc.diff, gc.stat, err = cg.readStagedDiff()
	if err != nil {
		return nil, fmt.Errorf("read staged diff: %w", err)
	}

	slog.Info("commit: staged diff",
		"diff_bytes", len(gc.diff),
		"stat_bytes", len(gc.stat),
		"result_staged", result.Staged,
	)

	if gc.diff == "" && gc.stat == "" {
		return nil, fmt.Errorf("no diff content for staged files %v; nothing to generate a message from", result.Staged)
	}

	// Step 4: Generate commit message via LLM.
	message, err := cg.generateMessage(ctx, gc, req.Hint, req.Context)
	if err != nil {
		return nil, fmt.Errorf("generate commit message (diff_bytes=%d, stat_bytes=%d, staged=%v): %w",
			len(gc.diff), len(gc.stat), result.Staged, err)
	}
	result.Message = message

	if req.DryRun {
		return result, nil
	}

	// Step 5: Commit.
	commitOut, err := cg.git("commit", "-m", message)
	if err != nil {
		result.HookError = strings.TrimSpace(commitOut)
		return result, fmt.Errorf("git commit failed: %s", strings.TrimSpace(commitOut))
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

	result.Duration = time.Since(start)
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

// gatherPreStageContext collects the file lists and recent log BEFORE staging.
// The diff is read later (after staging) via readStagedDiff().
func (cg *CommitGenerator) gatherPreStageContext() (*gitContext, error) {
	gc := &gitContext{}

	// Recent commit log for style matching.
	if out, err := cg.git("log", "--oneline", "-5"); err == nil {
		gc.recentLog = strings.TrimSpace(out)
	}

	// Already-staged files.
	if out, err := cg.git("diff", "--cached", "--name-only"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line != "" {
				gc.stagedFiles = append(gc.stagedFiles, line)
			}
		}
	}

	// All modified/untracked files from porcelain status.
	if out, err := cg.git("status", "--porcelain"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if len(line) < 4 {
				continue
			}
			path := strings.TrimSpace(line[3:])
			// Handle renames: "R  old -> new" — use the new path.
			if idx := strings.Index(path, " -> "); idx >= 0 {
				path = path[idx+4:]
			}
			if path != "" {
				gc.modifiedFiles = append(gc.modifiedFiles, path)
			}
		}
	}

	return gc, nil
}

// readStagedDiff reads the diff and stat from currently staged changes.
// Called after staging so that untracked files are included.
func (cg *CommitGenerator) readStagedDiff() (diff, stat string, err error) {
	out, gitErr := cg.git("diff", "--cached")
	if gitErr != nil {
		return "", "", fmt.Errorf("git diff --cached: %w", gitErr)
	}
	diff = strings.TrimSpace(out)

	out, gitErr = cg.git("diff", "--cached", "--stat")
	if gitErr != nil {
		return "", "", fmt.Errorf("git diff --cached --stat: %w", gitErr)
	}
	stat = strings.TrimSpace(out)
	return diff, stat, nil
}

// generateMessage builds the LLM prompt and makes one API call.
// Content assembly follows aider's proven pattern: context first, then diffs.
func (cg *CommitGenerator) generateMessage(ctx context.Context, gc *gitContext, hint, conversationContext string) (string, error) {
	var userContent strings.Builder

	// Context section — conversation history and/or hint explaining what
	// changes were made and why. Placed before diffs so the LLM reads the
	// intent before seeing the code changes (same order as aider).
	if conversationContext != "" {
		userContent.WriteString(conversationContext)
		userContent.WriteString("\n\n")
	}
	if hint != "" {
		userContent.WriteString("Context: ")
		userContent.WriteString(hint)
		userContent.WriteString("\n\n")
	}

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

	// Diffs section — prefixed with marker like aider does.
	userContent.WriteString("# Diffs:\n")
	diff := truncateDiff(gc.diff)
	if diff == "" {
		// If staged diff is empty (shouldn't happen after staging), use stat.
		diff = gc.stat
	}
	userContent.WriteString(diff)

	prompt := userContent.String()
	slog.Info("commit: LLM prompt",
		"prompt_bytes", len(prompt),
		"has_context", conversationContext != "",
		"has_hint", hint != "",
		"has_diff", gc.diff != "",
		"has_stat", gc.stat != "",
	)

	raw, err := cg.chain.SingleShot(ctx, commitSystemPrompt, prompt, commitMaxTokens)
	if err != nil {
		return "", err
	}

	return cleanCommitMessage(raw), nil
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
	out, err := cg.git(args...)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(out), err)
	}
	return nil
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
		fmt.Fprintf(&b, "✓ Committed: %s\n", r.Hash)
	}
	fmt.Fprintf(&b, "  Message: %s\n", firstLine(r.Message))

	if len(r.Staged) > 0 {
		fmt.Fprintf(&b, "  Files: %s\n", strings.Join(r.Staged, ", "))
	}

	if r.Duration > 0 {
		fmt.Fprintf(&b, "  Duration: %.1fs\n", r.Duration.Seconds())
	}

	if len(r.Remaining) > 0 {
		fmt.Fprintf(&b, "\n  Remaining uncommitted:\n")
		for _, f := range r.Remaining {
			fmt.Fprintf(&b, "    %s\n", f)
		}
	}

	return b.String()
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return first
}
