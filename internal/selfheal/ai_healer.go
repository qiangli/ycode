package selfheal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/api"
)

// AIHealer extends the basic healer with AI-driven error diagnosis and fixing.
// It sends error details to an AI provider, receives structured fix suggestions,
// validates and applies them, then triggers a rebuild.
type AIHealer struct {
	*Healer
	provider api.Provider
	config   *AIConfig
	// model is the model ID to use for healing requests.
	model string
}

// AIConfig extends Config with AI-specific options.
type AIConfig struct {
	*Config
	// MaxFixIterations is the maximum number of AI fix attempts per error.
	MaxFixIterations int
	// ConfirmBeforeApply asks user before applying AI-suggested fixes.
	ConfirmBeforeApply bool
	// IncludeContextFiles includes related files in the AI prompt.
	IncludeContextFiles bool
	// Model is the model ID to use (defaults to "claude-haiku-4-5-20251001").
	Model string
}

// DefaultAIConfig returns the default AI healer configuration.
func DefaultAIConfig() *AIConfig {
	return &AIConfig{
		Config:              DefaultConfig(),
		MaxFixIterations:    3,
		ConfirmBeforeApply:  true,
		IncludeContextFiles: true,
		Model:               "claude-haiku-4-5-20251001",
	}
}

// NewAIHealer creates a new AI-powered healer.
func NewAIHealer(cfg *AIConfig, provider api.Provider) *AIHealer {
	if cfg == nil {
		cfg = DefaultAIConfig()
	}
	model := cfg.Model
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	return &AIHealer{
		Healer:   NewHealer(cfg.Config),
		provider: provider,
		config:   cfg,
		model:    model,
	}
}

// FixAttempt represents a single AI fix attempt.
type FixAttempt struct {
	Iteration     int
	OriginalError string
	Analysis      string
	ProposedFixes []FileFix
	Applied       bool
	BuildResult   *BuildResult
	Success       bool
}

// FileFix represents a fix to a single file.
type FileFix struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Original    string `json:"original"`
	Modified    string `json:"modified"`
	Diff        string `json:"diff,omitempty"`
}

// BuildResult contains the result of a build attempt.
type BuildResult struct {
	Success bool
	Output  string
	Errors  []BuildError
}

// BuildError represents a single build error.
type BuildError struct {
	File    string
	Line    int
	Column  int
	Message string
}

const healingSystemPrompt = `You are a self-healing agent for a Go CLI application called ycode.
Your job is to analyze errors and produce structured fixes.

When given an error, you must:
1. Analyze the root cause
2. Determine which files need to be changed
3. Return a JSON array of fixes

IMPORTANT: Your response MUST contain a JSON code block with the fixes.
Use this exact format:

First, briefly explain the error and your fix (1-2 sentences).

Then provide the fixes:

` + "```json" + `
[
  {
    "path": "relative/path/to/file.go",
    "description": "Brief description of the fix",
    "original": "the exact original code that needs to be replaced",
    "modified": "the corrected code to replace it with"
  }
]
` + "```" + `

Rules:
- Paths must be relative to the project root
- "original" must be an exact substring of the current file content
- "modified" is the replacement text
- Only modify files with .go, .mod, or .sum extensions
- Never modify files in .git/, vendor/, or node_modules/
- Keep fixes minimal — change only what is necessary to fix the error
- If the error cannot be fixed by code changes (e.g., network issues, missing credentials), return an empty array: []
`

// AttemptAIFixing attempts to fix an error using the AI provider.
// It sends the error details to the AI, parses the response for fixes,
// validates paths, and applies the changes.
func (ah *AIHealer) AttemptAIFixing(ctx context.Context, errInfo ErrorInfo) (*FixAttempt, error) {
	if ah.provider == nil {
		return nil, fmt.Errorf("no AI provider configured for healing")
	}

	attempt := &FixAttempt{
		Iteration:     1,
		OriginalError: errInfo.Error.Error(),
	}

	for i := 0; i < ah.config.MaxFixIterations; i++ {
		attempt.Iteration = i + 1

		userPrompt := ah.buildUserPrompt(errInfo, attempt)

		response, err := ah.queryAI(ctx, userPrompt)
		if err != nil {
			return attempt, fmt.Errorf("AI query failed (iteration %d): %w", i+1, err)
		}

		attempt.Analysis = response

		fixes, err := ah.parseFixResponse(response)
		if err != nil {
			return attempt, fmt.Errorf("failed to parse AI response (iteration %d): %w", i+1, err)
		}

		if len(fixes) == 0 {
			return attempt, fmt.Errorf("AI determined this error cannot be fixed by code changes")
		}

		// Validate all paths before applying
		for _, fix := range fixes {
			if !ah.isPathHealable(fix.Path) {
				return attempt, fmt.Errorf("AI suggested modifying protected path: %s", fix.Path)
			}
		}

		attempt.ProposedFixes = fixes

		// Apply fixes
		if err := ah.applyFixes(fixes); err != nil {
			return attempt, fmt.Errorf("failed to apply fixes (iteration %d): %w", i+1, err)
		}
		attempt.Applied = true

		// Try to build
		buildOutput, buildErr := ah.tryBuild(ctx)
		attempt.BuildResult = &BuildResult{
			Success: buildErr == nil,
			Output:  buildOutput,
		}

		if buildErr == nil {
			attempt.Success = true
			return attempt, nil
		}

		// Build failed — feed errors back for next iteration
		attempt.BuildResult.Errors = ah.parseBuildErrors(buildOutput)
		errInfo = ErrorInfo{
			Type:    FailureTypeBuild,
			Error:   buildErr,
			Message: buildOutput,
			Context: errInfo.Context,
		}
	}

	return attempt, fmt.Errorf("AI healing failed after %d iterations", ah.config.MaxFixIterations)
}

// buildUserPrompt constructs the user message for the AI from the error info.
func (ah *AIHealer) buildUserPrompt(errInfo ErrorInfo, attempt *FixAttempt) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Error Type: %s\n", errInfo.Type))
	b.WriteString(fmt.Sprintf("Error Message: %s\n", errInfo.Message))

	if errInfo.StackTrace != "" {
		b.WriteString(fmt.Sprintf("\nStack Trace:\n%s\n", errInfo.StackTrace))
	}

	if len(errInfo.Context) > 0 {
		b.WriteString("\nContext:\n")
		for k, v := range errInfo.Context {
			b.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
		}
	}

	// If this is a retry after a failed build, include previous attempt info
	if attempt.Iteration > 1 && attempt.BuildResult != nil {
		b.WriteString(fmt.Sprintf("\nPrevious fix attempt %d failed with build errors:\n%s\n", attempt.Iteration-1, attempt.BuildResult.Output))
		b.WriteString("Please try a different approach.\n")
	}

	// Include file contents referenced in the error
	if ah.config.IncludeContextFiles {
		ah.appendFileContext(&b, errInfo)
	}

	return b.String()
}

// appendFileContext reads files referenced in the error and appends their content.
func (ah *AIHealer) appendFileContext(b *strings.Builder, errInfo ErrorInfo) {
	// Extract file paths from error message and context
	var files []string
	if f, ok := errInfo.Context["file"]; ok {
		files = append(files, f)
	}

	// Parse file paths from build error output (file.go:line:col: message)
	for _, line := range strings.Split(errInfo.Message, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) >= 2 && strings.HasSuffix(parts[0], ".go") {
			files = append(files, parts[0])
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	for _, f := range files {
		if seen[f] {
			continue
		}
		seen[f] = true

		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		b.WriteString(fmt.Sprintf("\n--- File: %s ---\n%s\n--- End: %s ---\n", f, string(content), f))
	}
}

// queryAI sends a prompt to the AI provider and collects the full text response.
func (ah *AIHealer) queryAI(ctx context.Context, userPrompt string) (string, error) {
	req := &api.Request{
		Model:     ah.model,
		MaxTokens: 4096,
		System:    healingSystemPrompt,
		Messages: []api.Message{
			{
				Role: api.RoleUser,
				Content: []api.ContentBlock{
					{Type: api.ContentTypeText, Text: userPrompt},
				},
			},
		},
		Stream: true,
	}

	events, errc := ah.provider.Send(ctx, req)
	return collectStreamingText(events, errc)
}

// collectStreamingText drains a streaming event channel and returns the
// accumulated text content.
func collectStreamingText(events <-chan *api.StreamEvent, errc <-chan error) (string, error) {
	var parts []string

	for ev := range events {
		if ev.Type != "content_block_delta" || ev.Delta == nil {
			continue
		}
		var delta struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(ev.Delta, &delta); err == nil && delta.Text != "" {
			parts = append(parts, delta.Text)
		}
	}

	if err := <-errc; err != nil {
		return "", fmt.Errorf("stream error: %w", err)
	}

	return strings.Join(parts, ""), nil
}

// tryBuild runs the build command and returns (output, error).
func (ah *AIHealer) tryBuild(ctx context.Context) (string, error) {
	if ah.config.BuildCommand == "" {
		return "", fmt.Errorf("no build command configured")
	}

	parts := strings.Fields(ah.config.BuildCommand)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty build command")
	}

	cmd := execCommand(ctx, parts[0], parts[1:]...)
	cmd.Dir = ah.findProjectRoot()

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// applyFixes applies the generated fixes to the codebase.
func (ah *AIHealer) applyFixes(fixes []FileFix) error {
	for _, fix := range fixes {
		if !ah.isPathHealable(fix.Path) {
			return fmt.Errorf("path %s is not in healable paths", fix.Path)
		}

		if fix.Original == "" && fix.Modified == "" {
			continue
		}

		// If Original is empty, this is a new file
		if fix.Original == "" {
			dir := filepath.Dir(fix.Path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
			if err := os.WriteFile(fix.Path, []byte(fix.Modified), 0644); err != nil {
				return fmt.Errorf("failed to write new file %s: %w", fix.Path, err)
			}
			continue
		}

		// Read current content
		content, err := os.ReadFile(fix.Path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", fix.Path, err)
		}

		// Replace original with modified
		current := string(content)
		if !strings.Contains(current, fix.Original) {
			return fmt.Errorf("original content not found in %s", fix.Path)
		}

		updated := strings.Replace(current, fix.Original, fix.Modified, 1)
		if err := os.WriteFile(fix.Path, []byte(updated), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", fix.Path, err)
		}
	}

	return nil
}

// parseBuildErrors extracts structured errors from build output.
func (ah *AIHealer) parseBuildErrors(output string) []BuildError {
	var errors []BuildError
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Go build error format: file.go:line:col: message
		parts := strings.SplitN(line, ":", 4)
		if len(parts) < 3 {
			continue
		}
		// Heuristic: first part must look like a Go file path
		if !strings.HasSuffix(parts[0], ".go") {
			continue
		}

		file := parts[0]
		lineNum := 0
		colNum := 0
		fmt.Sscanf(parts[1], "%d", &lineNum)
		if lineNum == 0 {
			continue // Not a valid line number — skip
		}

		message := ""
		if len(parts) >= 4 {
			fmt.Sscanf(parts[2], "%d", &colNum)
			message = strings.TrimSpace(parts[3])
		} else {
			message = strings.TrimSpace(parts[2])
		}

		errors = append(errors, BuildError{
			File:    file,
			Line:    lineNum,
			Column:  colNum,
			Message: message,
		})
	}

	return errors
}

// parseFixResponse parses the AI's fix response into structured fixes.
func (ah *AIHealer) parseFixResponse(response string) ([]FileFix, error) {
	// Try to parse as raw JSON first
	var fixes []FileFix
	if err := json.Unmarshal([]byte(response), &fixes); err == nil {
		return fixes, nil
	}

	// Try to extract JSON from markdown code block
	if idx := strings.Index(response, "```json"); idx != -1 {
		start := idx + 7
		if end := strings.Index(response[start:], "```"); end != -1 {
			jsonContent := strings.TrimSpace(response[start : start+end])
			if err := json.Unmarshal([]byte(jsonContent), &fixes); err == nil {
				return fixes, nil
			}
		}
	}

	// Try plain ``` block (AI might omit the language tag)
	if idx := strings.Index(response, "```\n["); idx != -1 {
		start := idx + 4
		if end := strings.Index(response[start:], "```"); end != -1 {
			jsonContent := strings.TrimSpace(response[start : start+end])
			if err := json.Unmarshal([]byte(jsonContent), &fixes); err == nil {
				return fixes, nil
			}
		}
	}

	return nil, fmt.Errorf("could not parse AI fix response")
}

// isPathHealable checks if a path is allowed to be modified.
func (ah *AIHealer) isPathHealable(path string) bool {
	// Check protected paths first
	for _, protected := range ah.config.ProtectedPaths {
		if strings.Contains(path, protected) {
			return false
		}
	}

	// Check healable paths — if specified, path must match at least one pattern
	if len(ah.config.HealablePaths) > 0 {
		for _, pattern := range ah.config.HealablePaths {
			if matched, _ := filepath.Match(pattern, path); matched {
				return true
			}
			if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
				return true
			}
		}
		return false
	}

	return true
}
