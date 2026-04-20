package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// initMaxTokens limits LLM output for AGENTS.md generation.
	initMaxTokens = 2048

	// initSystemPrompt guides the LLM to generate concise AGENTS.md.
	initSystemPrompt = `You are an expert at creating developer documentation. Given project context, generate a concise AGENTS.md file.

AGENTS.md provides guidance to AI coding assistants working in the repository. Every line must answer: "Would an agent likely miss this without help?"

Output format:
- Start with: # AGENTS.md followed by a blank line
- One-sentence project description
- Reference key files if they exist (CLAUDE.md, USAGE.md, README.md)
- Quick reference section with exact build/test/lint commands found in the project
- Project-specific notes: non-obvious architecture, testing quirks, conventions differing from defaults
- Keep under 40 lines
- Use bullet points, not prose
- Never fabricate commands - only include what was verified from the project files

Reference files like this:
**Read [CLAUDE.md](./CLAUDE.md)** for additional guidance.

Example quick reference format:
` + "```bash\n" +
		"make build    # quality gate: tidy -> fmt -> vet -> test\n" +
		"make test     # unit tests only\n" +
		"```"
)

// InitGenerator creates AGENTS.md with single-shot LLM call.
type InitGenerator struct {
	chain *ModelChain
	cwd   string
}

// NewInitGenerator creates an InitGenerator.
func NewInitGenerator(chain *ModelChain, cwd string) *InitGenerator {
	return &InitGenerator{chain: chain, cwd: cwd}
}

// InitResult holds the outcome of init generation.
type InitResult struct {
	Content   string
	FilesRead []string
}

// Generate creates AGENTS.md via single LLM call.
// It reads relevant files from the project and generates instructions.
func (ig *InitGenerator) Generate(ctx context.Context, focus string) (*InitResult, error) {
	// Gather project context by reading files directly.
	context := ig.gatherContext()
	context.Focus = focus

	// Build user prompt with all context.
	prompt := buildInitPrompt(context)

	// Single LLM call - no tools, no conversation history.
	content, err := ig.chain.SingleShot(ctx, initSystemPrompt, prompt, initMaxTokens)
	if err != nil {
		return nil, fmt.Errorf("generate AGENTS.md: %w", err)
	}

	return &InitResult{
		Content:   cleanInitOutput(content),
		FilesRead: context.FilesRead,
	}, nil
}

// initContext holds gathered project information.
type initContext struct {
	ProjectName      string
	Languages        []string
	BuildCmd         string
	TestCmd          string
	LintCmd          string
	HasREADME        bool
	HasUSAGE         bool
	HasCLAUDE        bool
	HasAGENTS        bool
	HasMakefile      bool
	GoModContent     string
	PkgJSONContent   string
	PyProjectContent string
	CargoContent     string
	CIFiles          map[string]string // filename -> content
	Focus            string
	FilesRead        []string
}

// gatherContext reads relevant project files.
func (ig *InitGenerator) gatherContext() *initContext {
	ctx := &initContext{
		CIFiles:   make(map[string]string),
		Languages: detectLanguages(ig.cwd),
	}

	// Read key documentation files.
	if content, ok := ig.readFile("README.md"); ok {
		ctx.HasREADME = true
		ctx.FilesRead = append(ctx.FilesRead, "README.md")
		// Extract first line as project description.
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				ctx.ProjectName = line
				break
			}
		}
		// If no plain line found, use first h1.
		if ctx.ProjectName == "" {
			for _, line := range lines {
				if strings.HasPrefix(line, "# ") {
					ctx.ProjectName = strings.TrimPrefix(line, "# ")
					break
				}
			}
		}
	}

	if _, ok := ig.readFile("USAGE.md"); ok {
		ctx.HasUSAGE = true
		ctx.FilesRead = append(ctx.FilesRead, "USAGE.md")
	}

	if _, ok := ig.readFile("CLAUDE.md"); ok {
		ctx.HasCLAUDE = true
		ctx.FilesRead = append(ctx.FilesRead, "CLAUDE.md")
	}

	if _, ok := ig.readFile("AGENTS.md"); ok {
		ctx.HasAGENTS = true
	}

	// Read manifest files to extract commands.
	if content, ok := ig.readFile("Makefile"); ok {
		ctx.HasMakefile = true
		ctx.BuildCmd, ctx.TestCmd, ctx.LintCmd = extractMakeTargets(content)
		ctx.FilesRead = append(ctx.FilesRead, "Makefile")
	}

	if content, ok := ig.readFile("go.mod"); ok {
		ctx.GoModContent = truncateContent(content, 50)
		ctx.FilesRead = append(ctx.FilesRead, "go.mod")
		if ctx.BuildCmd == "" {
			ctx.BuildCmd = "go build ./..."
		}
		if ctx.TestCmd == "" {
			ctx.TestCmd = "go test -race ./..."
		}
		if ctx.LintCmd == "" {
			ctx.LintCmd = "go vet ./..."
		}
	}

	if content, ok := ig.readFile("package.json"); ok {
		ctx.PkgJSONContent = truncateContent(content, 100)
		ctx.FilesRead = append(ctx.FilesRead, "package.json")
		build, test, lint := extractPkgJSONScripts(content)
		if ctx.BuildCmd == "" {
			ctx.BuildCmd = build
		}
		if ctx.TestCmd == "" {
			ctx.TestCmd = test
		}
		if ctx.LintCmd == "" {
			ctx.LintCmd = lint
		}
	}

	if content, ok := ig.readFile("pyproject.toml"); ok {
		ctx.PyProjectContent = truncateContent(content, 50)
		ctx.FilesRead = append(ctx.FilesRead, "pyproject.toml")
		if ctx.BuildCmd == "" {
			ctx.BuildCmd = "python -m build"
		}
		if ctx.TestCmd == "" {
			ctx.TestCmd = "pytest"
		}
		if ctx.LintCmd == "" {
			ctx.LintCmd = "ruff check ."
		}
	}

	if content, ok := ig.readFile("Cargo.toml"); ok {
		ctx.CargoContent = truncateContent(content, 50)
		ctx.FilesRead = append(ctx.FilesRead, "Cargo.toml")
		if ctx.BuildCmd == "" {
			ctx.BuildCmd = "cargo build"
		}
		if ctx.TestCmd == "" {
			ctx.TestCmd = "cargo test"
		}
		if ctx.LintCmd == "" {
			ctx.LintCmd = "cargo clippy"
		}
	}

	// Read CI configs for testing insights.
	ciFiles := []string{
		".github/workflows/ci.yml",
		".github/workflows/test.yml",
		".github/workflows/build.yml",
		".gitlab-ci.yml",
		"azure-pipelines.yml",
		"Jenkinsfile",
	}
	for _, f := range ciFiles {
		if content, ok := ig.readFile(f); ok {
			ctx.CIFiles[f] = truncateContent(content, 30)
			ctx.FilesRead = append(ctx.FilesRead, f)
		}
	}

	return ctx
}

// readFile reads a file relative to cwd.
func (ig *InitGenerator) readFile(name string) (string, bool) {
	path := filepath.Join(ig.cwd, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// truncateContent limits content to approximately n lines.
func truncateContent(content string, maxLines int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}
	return strings.Join(lines[:maxLines], "\n") + "\n... [truncated]"
}

// buildInitPrompt constructs the user prompt for the LLM.
func buildInitPrompt(ctx *initContext) string {
	var b strings.Builder

	b.WriteString("Generate an AGENTS.md file for this project.\n\n")

	if ctx.Focus != "" {
		b.WriteString(fmt.Sprintf("User focus: %s\n\n", ctx.Focus))
	}

	b.WriteString("## Project Overview\n\n")

	if ctx.ProjectName != "" {
		b.WriteString(fmt.Sprintf("Project: %s\n", ctx.ProjectName))
	}

	if len(ctx.Languages) > 0 {
		b.WriteString(fmt.Sprintf("Languages: %s\n", strings.Join(ctx.Languages, ", ")))
	}

	b.WriteString("\n## Available Documentation\n\n")
	if ctx.HasREADME {
		b.WriteString("- README.md exists\n")
	}
	if ctx.HasUSAGE {
		b.WriteString("- USAGE.md exists (reference for detailed commands)\n")
	}
	if ctx.HasCLAUDE {
		b.WriteString("- CLAUDE.md exists (reference for Claude-specific guidance)\n")
	}
	if ctx.HasAGENTS {
		b.WriteString("- AGENTS.md already exists (enhance it)\n")
	}

	b.WriteString("\n## Build/Test Commands\n\n")
	if ctx.BuildCmd != "" {
		b.WriteString(fmt.Sprintf("Build: %s\n", ctx.BuildCmd))
	}
	if ctx.TestCmd != "" {
		b.WriteString(fmt.Sprintf("Test: %s\n", ctx.TestCmd))
	}
	if ctx.LintCmd != "" {
		b.WriteString(fmt.Sprintf("Lint: %s\n", ctx.LintCmd))
	}

	// Include relevant file snippets.
	if ctx.GoModContent != "" {
		b.WriteString("\n## go.mod\n\n```\n")
		b.WriteString(ctx.GoModContent)
		b.WriteString("\n```\n")
	}

	if ctx.PkgJSONContent != "" {
		b.WriteString("\n## package.json\n\n```json\n")
		b.WriteString(ctx.PkgJSONContent)
		b.WriteString("\n```\n")
	}

	if ctx.HasMakefile {
		b.WriteString("\n## Makefile Targets\n\n")
		b.WriteString(fmt.Sprintf("Key targets found: build=%s, test=%s, lint=%s\n",
			ctx.BuildCmd, ctx.TestCmd, ctx.LintCmd))
	}

	if len(ctx.CIFiles) > 0 {
		b.WriteString("\n## CI Configuration\n\n")
		for name, content := range ctx.CIFiles {
			b.WriteString(fmt.Sprintf("%s:\n```yaml\n%s\n```\n", name, content))
		}
	}

	b.WriteString("\n## Output\n\n")
	b.WriteString("Generate the AGENTS.md content now. Be concise and accurate. " +
		"Only include commands and facts verified from the files above.")

	return b.String()
}

// cleanInitOutput extracts clean markdown from LLM response.
func cleanInitOutput(raw string) string {
	content := strings.TrimSpace(raw)

	// Strip markdown fences if present.
	if strings.HasPrefix(content, "```markdown") {
		content = strings.TrimPrefix(content, "```markdown")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	} else if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
		}
		if len(lines) >= 1 && strings.HasPrefix(lines[len(lines)-1], "```") {
			lines = lines[:len(lines)-1]
		}
		content = strings.TrimSpace(strings.Join(lines, "\n"))
	}

	return content
}

// detectLanguages identifies project languages from file presence.
func detectLanguages(cwd string) []string {
	var langs []string
	files := map[string]string{
		"go.mod":           "Go",
		"Cargo.toml":       "Rust",
		"package.json":     "JavaScript/Node.js",
		"pyproject.toml":   "Python",
		"setup.py":         "Python",
		"requirements.txt": "Python",
		"pom.xml":          "Java",
		"build.gradle":     "Java",
		"Gemfile":          "Ruby",
		"composer.json":    "PHP",
	}
	for file, lang := range files {
		if _, err := os.Stat(filepath.Join(cwd, file)); err == nil {
			langs = append(langs, lang)
		}
	}
	return langs
}

// extractMakeTargets extracts common targets from Makefile.
func extractMakeTargets(content string) (build, test, lint string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "build:") || strings.HasPrefix(line, "all:") {
			build = "make build"
		}
		if strings.HasPrefix(line, "test:") {
			test = "make test"
		}
		if strings.HasPrefix(line, "lint:") || strings.HasPrefix(line, "check:") {
			lint = "make lint"
		}
	}
	return
}

// extractPkgJSONScripts extracts scripts from package.json content.
func extractPkgJSONScripts(content string) (build, test, lint string) {
	// Simple string extraction - look for script definitions.
	if strings.Contains(content, `"build"`) {
		build = "npm run build"
	}
	if strings.Contains(content, `"test"`) {
		test = "npm test"
	}
	if strings.Contains(content, `"lint"`) {
		lint = "npm run lint"
	}
	return
}
