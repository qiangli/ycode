package builtin

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed init_template.txt
var initTemplate string

const (
	// initMaxTokens limits LLM output for AGENTS.md generation.
	initMaxTokens = 8192

	// initSystemPrompt provides the LLM with its role.
	initSystemPrompt = `You are an expert at creating AGENTS.md files for software repositories.
Your task is to analyze project context and generate a concise, high-quality AGENTS.md file.
Follow the investigation guidance and writing rules provided in the user prompt.

Output format:
- Output ONLY the raw markdown content for AGENTS.md. No wrapper, no explanation.
- Do NOT wrap the output in code fences (no ` + "```" + `markdown ... ` + "```" + `). The output is written directly to the file.
- The file header MUST be "# AGENTS.md" (not "# CLAUDE.md"). This is a tool-agnostic instruction file.
- Include "This file provides guidance to AI coding assistants working in this repository." as the first line after the header.

Key principles:
- Every line must earn its place. If an agent would figure it out without help, omit it.
- Delegate to sub-directory instruction files instead of duplicating their content.
- Never include speculative "Open Questions" sections or content you cannot verify.
- Prefer pointers ("see X for details") over inline tables or verbose lists.

Architecture depth:
- For multi-service repos, include a service table with tech stack AND port numbers.
- Document entry points with the actual call chain (e.g., main.go → cmd.Execute(embeddedAssets)).
- Describe non-obvious internal packages — an agent can read filenames but not infer purpose.
- Document build patterns like embedded assets, code generation, or rsync-then-embed flows.
- Include operational commands: hot-reload endpoints, apply scripts, data persistence warnings.
- If .env files are machine-specific and gitignored, say so explicitly.
- Summarize key scripts in a table when scripts/ contains non-trivial operational tooling.`
)

// InitGenerator creates AGENTS.md with single-shot LLM call.
type InitGenerator struct {
	cwd string
}

// NewInitGenerator creates an InitGenerator.
func NewInitGenerator(cwd string) *InitGenerator {
	return &InitGenerator{cwd: cwd}
}

// InitResult holds the outcome of init generation.
type InitResult struct {
	SystemPrompt   string // System prompt for LLM role
	UserPrompt     string // User prompt with instructions and context
	FilesRead      []string
	Questions      []string // Questions to ask the user
	MissingContext []string // Missing context that couldn't be inferred
}

// Generate prepares all context and builds the prompts for AGENTS.md generation.
// Returns both system prompt (LLM role) and user prompt (instructions + context).
// The actual LLM call is made by the caller using their preferred provider.
func (ig *InitGenerator) Generate(args string) (*InitResult, error) {
	// Gather project context by reading files directly.
	ctx := ig.gatherContext()

	// Build the user prompt from template.
	userPrompt := ig.buildPrompt(ctx, args)

	// Identify potential questions based on missing context.
	questions := ig.identifyQuestions(ctx)

	return &InitResult{
		SystemPrompt:   initSystemPrompt,
		UserPrompt:     userPrompt,
		FilesRead:      ctx.FilesRead,
		Questions:      questions,
		MissingContext: ctx.MissingContext,
	}, nil
}

// initContext holds gathered project information.
type initContext struct {
	ProjectName      string
	Languages        []string
	Frameworks       []string
	BuildCmd         string
	TestCmd          string
	LintCmd          string
	TypecheckCmd     string
	HasREADME        bool
	HasUSAGE         bool
	HasCLAUDE        bool
	HasAGENTS        bool
	HasMakefile      bool
	READMEContent    string
	GoModContent     string
	PkgJSONContent   string
	PyProjectContent string
	CargoContent     string
	DockerContent    string
	CIFiles          map[string]string // filename -> content
	ConfigFiles      map[string]string // other config files
	SubInstructions  []string          // sub-directory AGENTS.md/CLAUDE.md paths
	Scripts          map[string]string // script path -> header/purpose lines
	ComposeFiles     map[string]string // docker-compose path -> content
	EntryPoints      map[string]string // main.go / cmd/ entry -> content
	ToolVersions     string            // mise.toml or .tool-versions content
	Focus            string
	FilesRead        []string
	MissingContext   []string
}

// gatherContext reads relevant project files following opencode's priority order.
func (ig *InitGenerator) gatherContext() *initContext {
	ctx := &initContext{
		CIFiles:      make(map[string]string),
		ConfigFiles:  make(map[string]string),
		Scripts:      make(map[string]string),
		ComposeFiles: make(map[string]string),
		EntryPoints:  make(map[string]string),
		Languages:    []string{},
	}

	// Phase 1: Read highest-value sources first (README, manifests)
	ig.readREADME(ctx)
	ig.readManifests(ctx)

	// Phase 2: Read build/test config
	ig.readBuildConfig(ctx)

	// Phase 3: Read CI/workflows
	ig.readCIConfig(ctx)

	// Phase 4: Read existing instruction files
	ig.readInstructionFiles(ctx)

	// Phase 5: Discover sub-directory instruction files
	ig.discoverSubInstructions(ctx)

	// Phase 6: Detect frameworks
	ctx.Frameworks = ig.detectFrameworks()

	// Phase 7: Read scripts for operational context
	ig.readScripts(ctx)

	// Phase 8: Read docker-compose files for service topology
	ig.readComposeFiles(ctx)

	// Phase 9: Read entry points for architecture understanding
	ig.readEntryPoints(ctx)

	// Phase 10: Read tool version files
	ig.readToolVersions(ctx)

	return ctx
}

func (ig *InitGenerator) readREADME(ctx *initContext) {
	// Try multiple README variants
	variants := []string{"README.md", "README.txt", "README.rst", "README"}
	for _, name := range variants {
		if content, ok := ig.readFile(name); ok {
			ctx.HasREADME = true
			ctx.READMEContent = truncateContent(content, 80)
			ctx.FilesRead = append(ctx.FilesRead, name)
			ctx.ProjectName = ig.extractProjectName(content, name)
			break
		}
	}
}

func (ig *InitGenerator) readManifests(ctx *initContext) {
	// go.mod
	if content, ok := ig.readFile("go.mod"); ok {
		ctx.GoModContent = truncateContent(content, 30)
		ctx.FilesRead = append(ctx.FilesRead, "go.mod")
		ctx.Languages = append(ctx.Languages, "Go")
		ctx.BuildCmd = "go build ./..."
		ctx.TestCmd = "go test -race ./..."
		ctx.LintCmd = "go vet ./..."
		ctx.TypecheckCmd = "go build ./..."
	}

	// package.json
	if content, ok := ig.readFile("package.json"); ok {
		ctx.PkgJSONContent = truncateContent(content, 50)
		ctx.FilesRead = append(ctx.FilesRead, "package.json")
		ctx.Languages = append(ctx.Languages, ig.detectJSLanguage(content))

		// Extract scripts
		scripts := ig.extractNPMScripts(content)
		if ctx.BuildCmd == "" {
			ctx.BuildCmd = scripts["build"]
		}
		if ctx.TestCmd == "" {
			ctx.TestCmd = scripts["test"]
		}
		if ctx.LintCmd == "" {
			ctx.LintCmd = scripts["lint"]
		}
	}

	// pyproject.toml / setup.py / requirements.txt
	if content, ok := ig.readFile("pyproject.toml"); ok {
		ctx.PyProjectContent = truncateContent(content, 30)
		ctx.FilesRead = append(ctx.FilesRead, "pyproject.toml")
		ctx.Languages = append(ctx.Languages, "Python")
		if ctx.BuildCmd == "" {
			ctx.BuildCmd = "python -m build"
		}
		if ctx.TestCmd == "" {
			ctx.TestCmd = "pytest"
		}
		if ctx.LintCmd == "" {
			ctx.LintCmd = "ruff check ."
		}
	} else if _, ok := ig.readFile("setup.py"); ok {
		ctx.FilesRead = append(ctx.FilesRead, "setup.py")
		ctx.Languages = append(ctx.Languages, "Python")
	} else if _, ok := ig.readFile("requirements.txt"); ok {
		ctx.FilesRead = append(ctx.FilesRead, "requirements.txt")
		ctx.Languages = append(ctx.Languages, "Python")
	}

	// Cargo.toml
	if content, ok := ig.readFile("Cargo.toml"); ok {
		ctx.CargoContent = truncateContent(content, 30)
		ctx.FilesRead = append(ctx.FilesRead, "Cargo.toml")
		ctx.Languages = append(ctx.Languages, "Rust")
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

	// Dockerfile
	if content, ok := ig.readFile("Dockerfile"); ok {
		ctx.DockerContent = truncateContent(content, 20)
		ctx.FilesRead = append(ctx.FilesRead, "Dockerfile")
	}
}

func (ig *InitGenerator) readBuildConfig(ctx *initContext) {
	// Makefile
	if content, ok := ig.readFile("Makefile"); ok {
		ctx.HasMakefile = true
		ctx.FilesRead = append(ctx.FilesRead, "Makefile")
		ctx.ConfigFiles["Makefile"] = truncateContent(content, 150)

		// Extract common targets. Makefile targets override language
		// defaults because Makefiles often wrap raw commands with
		// project-specific flags, paths, or prerequisites.
		targets := ig.extractMakeTargets(content)
		if targets["build"] != "" {
			ctx.BuildCmd = "make build"
		}
		if targets["test"] != "" {
			ctx.TestCmd = "make test"
		}
		if targets["lint"] != "" {
			ctx.LintCmd = "make lint"
		}
	}

	// Taskfile (task runner)
	if content, ok := ig.readFile("Taskfile.yml"); ok {
		ctx.ConfigFiles["Taskfile.yml"] = truncateContent(content, 30)
		ctx.FilesRead = append(ctx.FilesRead, "Taskfile.yml")
	}

	// Justfile
	if content, ok := ig.readFile("justfile"); ok {
		ctx.ConfigFiles["justfile"] = truncateContent(content, 20)
		ctx.FilesRead = append(ctx.FilesRead, "justfile")
	}
}

func (ig *InitGenerator) readCIConfig(ctx *initContext) {
	// GitHub Actions
	ciDir := ".github/workflows"
	entries, err := os.ReadDir(filepath.Join(ig.cwd, ciDir))
	if err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".yml") || strings.HasSuffix(entry.Name(), ".yaml") {
				name := filepath.Join(ciDir, entry.Name())
				if content, ok := ig.readFile(name); ok {
					ctx.CIFiles[name] = truncateContent(content, 40)
					ctx.FilesRead = append(ctx.FilesRead, name)
				}
			}
		}
	}

	// Other CI configs
	ciFiles := []string{
		".gitlab-ci.yml",
		"azure-pipelines.yml",
		"Jenkinsfile",
		".circleci/config.yml",
		".buildkite/pipeline.yml",
	}
	for _, name := range ciFiles {
		if content, ok := ig.readFile(name); ok {
			ctx.CIFiles[name] = truncateContent(content, 40)
			ctx.FilesRead = append(ctx.FilesRead, name)
		}
	}
}

func (ig *InitGenerator) readInstructionFiles(ctx *initContext) {
	// Existing AGENTS.md — read in full so the LLM can improve in place.
	if content, ok := ig.readFile("AGENTS.md"); ok {
		ctx.HasAGENTS = true
		ctx.FilesRead = append(ctx.FilesRead, "AGENTS.md")
		ctx.ConfigFiles["AGENTS.md"] = truncateContent(content, 200)
	}

	// CLAUDE.md
	if content, ok := ig.readFile("CLAUDE.md"); ok {
		ctx.HasCLAUDE = true
		ctx.FilesRead = append(ctx.FilesRead, "CLAUDE.md")
		ctx.ConfigFiles["CLAUDE.md"] = truncateContent(content, 100)
	}

	// USAGE.md
	if _, ok := ig.readFile("USAGE.md"); ok {
		ctx.HasUSAGE = true
		ctx.FilesRead = append(ctx.FilesRead, "USAGE.md")
	}

	// .cursorrules
	if _, ok := ig.readFile(".cursorrules"); ok {
		ctx.FilesRead = append(ctx.FilesRead, ".cursorrules")
	}

	// .cursor/rules
	if _, ok := ig.readFile(".cursor/rules"); ok {
		ctx.FilesRead = append(ctx.FilesRead, ".cursor/rules")
	}
}

// discoverSubInstructions walks one level of subdirectories looking for
// AGENTS.md or CLAUDE.md files so the LLM knows they exist and can
// delegate to them rather than duplicating their content.
func (ig *InitGenerator) discoverSubInstructions(ctx *initContext) {
	entries, err := os.ReadDir(ig.cwd)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			rel := filepath.Join(entry.Name(), name)
			if _, err := os.Stat(filepath.Join(ig.cwd, rel)); err == nil {
				ctx.SubInstructions = append(ctx.SubInstructions, rel)
			}
		}
		// Also check one more level (e.g., docker/metrics/AGENTS.md).
		subEntries, err := os.ReadDir(filepath.Join(ig.cwd, entry.Name()))
		if err != nil {
			continue
		}
		for _, sub := range subEntries {
			if !sub.IsDir() || strings.HasPrefix(sub.Name(), ".") {
				continue
			}
			for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
				rel := filepath.Join(entry.Name(), sub.Name(), name)
				if _, err := os.Stat(filepath.Join(ig.cwd, rel)); err == nil {
					ctx.SubInstructions = append(ctx.SubInstructions, rel)
				}
			}
		}
	}
}

// readScripts reads shell scripts from common directories to capture
// operational context (what each script does). Only the first ~10 lines
// are kept to extract purpose comments and usage patterns.
func (ig *InitGenerator) readScripts(ctx *initContext) {
	scriptDirs := []string{"scripts", "script", "bin", "hack", "tools"}
	for _, dir := range scriptDirs {
		entries, err := os.ReadDir(filepath.Join(ig.cwd, dir))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			// Only read shell scripts and common automation files.
			if !strings.HasSuffix(name, ".sh") &&
				!strings.HasSuffix(name, ".bash") &&
				!strings.HasSuffix(name, ".zsh") {
				continue
			}
			rel := filepath.Join(dir, name)
			if content, ok := ig.readFile(rel); ok {
				ctx.Scripts[rel] = truncateContent(content, 10)
				ctx.FilesRead = append(ctx.FilesRead, rel)
			}
		}
	}
}

// readComposeFiles finds docker-compose files for service topology.
func (ig *InitGenerator) readComposeFiles(ctx *initContext) {
	// Check root-level compose files.
	composeNames := []string{
		"docker-compose.yml", "docker-compose.yaml",
		"compose.yml", "compose.yaml",
	}
	for _, name := range composeNames {
		if content, ok := ig.readFile(name); ok {
			ctx.ComposeFiles[name] = truncateContent(content, 80)
			ctx.FilesRead = append(ctx.FilesRead, name)
		}
	}

	// Check docker/ subdirectory (common location).
	dockerDirs := []string{"docker", "deploy", "infra"}
	for _, dir := range dockerDirs {
		entries, err := os.ReadDir(filepath.Join(ig.cwd, dir))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			for _, name := range composeNames {
				rel := filepath.Join(dir, entry.Name(), name)
				if content, ok := ig.readFile(rel); ok {
					ctx.ComposeFiles[rel] = truncateContent(content, 80)
					ctx.FilesRead = append(ctx.FilesRead, rel)
				}
			}
		}
	}
}

// readEntryPoints reads main.go files and cmd/ directory structure to
// understand application architecture and entry points.
func (ig *InitGenerator) readEntryPoints(ctx *initContext) {
	// Root main.go.
	if content, ok := ig.readFile("main.go"); ok {
		ctx.EntryPoints["main.go"] = truncateContent(content, 30)
		ctx.FilesRead = append(ctx.FilesRead, "main.go")
	}

	// cmd/ directory — each subdirectory is typically a separate binary.
	ig.readCmdDir(ctx, "cmd")

	// Check for main.go in common service directories.
	serviceDirs := []string{"server", "client", "api", "app", "web", "worker"}
	for _, dir := range serviceDirs {
		rel := filepath.Join(dir, "main.go")
		if content, ok := ig.readFile(rel); ok {
			ctx.EntryPoints[rel] = truncateContent(content, 30)
			ctx.FilesRead = append(ctx.FilesRead, rel)
		}
		// Also check cmd/ inside service dirs.
		ig.readCmdDir(ctx, filepath.Join(dir, "cmd"))
	}
}

// readCmdDir reads main.go files under a cmd/ directory structure.
func (ig *InitGenerator) readCmdDir(ctx *initContext, cmdDir string) {
	entries, err := os.ReadDir(filepath.Join(ig.cwd, cmdDir))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		rel := filepath.Join(cmdDir, entry.Name(), "main.go")
		if content, ok := ig.readFile(rel); ok {
			ctx.EntryPoints[rel] = truncateContent(content, 30)
			ctx.FilesRead = append(ctx.FilesRead, rel)
		}
	}
}

// readToolVersions reads mise.toml, .tool-versions, or similar files
// that pin tool/runtime versions.
func (ig *InitGenerator) readToolVersions(ctx *initContext) {
	versionFiles := []string{"mise.toml", ".mise.toml", ".tool-versions", ".node-version", ".go-version", ".python-version", ".ruby-version"}
	for _, name := range versionFiles {
		if content, ok := ig.readFile(name); ok {
			ctx.ToolVersions += fmt.Sprintf("### %s\n```\n%s\n```\n\n", name, truncateContent(content, 20))
			ctx.FilesRead = append(ctx.FilesRead, name)
		}
	}
}

func (ig *InitGenerator) detectFrameworks() []string {
	var frameworks []string

	// Check for framework indicators
	indicators := map[string]string{
		"next.config.js":       "Next.js",
		"next.config.ts":       "Next.js",
		"svelte.config.js":     "Svelte",
		"angular.json":         "Angular",
		"vue.config.js":        "Vue",
		"nuxt.config.ts":       "Nuxt",
		"astro.config.mjs":     "Astro",
		"remix.config.js":      "Remix",
		"gatsby-config.js":     "Gatsby",
		"vite.config.ts":       "Vite",
		"vite.config.js":       "Vite",
		"tailwind.config.js":   "Tailwind CSS",
		"tailwind.config.ts":   "Tailwind CSS",
		"prisma/schema.prisma": "Prisma",
		"drizzle.config.ts":    "Drizzle",
		"docker-compose.yml":   "Docker Compose",
		"docker-compose.yaml":  "Docker Compose",
		"compose.yml":          "Docker Compose",
		"compose.yaml":         "Docker Compose",
		"k8s":                  "Kubernetes",
		"terraform":            "Terraform",
	}

	for file, framework := range indicators {
		if _, err := os.Stat(filepath.Join(ig.cwd, file)); err == nil {
			frameworks = append(frameworks, framework)
		}
	}

	return frameworks
}

func (ig *InitGenerator) identifyQuestions(ctx *initContext) []string {
	var questions []string

	// Only suggest questions for truly missing context
	if !ctx.HasREADME && ctx.ProjectName == "" {
		questions = append(questions, "What is the name/purpose of this project?")
	}

	if ctx.BuildCmd == "" && len(ctx.Languages) > 0 {
		questions = append(questions, fmt.Sprintf("What is the build command for this %s project?", strings.Join(ctx.Languages, "/")))
	}

	if ctx.TestCmd == "" {
		questions = append(questions, "How do you run tests in this project?")
	}

	// Check for common missing setup
	if _, ok := ig.readFile(".env.example"); ok {
		if _, hasEnv := ig.readFile(".env"); !hasEnv {
			questions = append(questions, "Are there required environment variables from .env.example?")
		}
	}

	return questions
}

// extractProjectName extracts project name from README content.
func (ig *InitGenerator) extractProjectName(content, filename string) string {
	lines := strings.Split(content, "\n")

	// Try first h1
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}

	// Fallback to first non-empty line
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}

	return ""
}

// detectJSLanguage detects if it's TypeScript or JavaScript.
func (ig *InitGenerator) detectJSLanguage(pkgJSON string) string {
	if strings.Contains(pkgJSON, "typescript") ||
		ig.fileExists("tsconfig.json") {
		return "TypeScript"
	}
	return "JavaScript"
}

// extractNPMScripts extracts npm scripts from package.json.
func (ig *InitGenerator) extractNPMScripts(content string) map[string]string {
	scripts := make(map[string]string)

	// Simple extraction - look for script names
	scriptNames := []string{"build", "test", "lint", "dev", "start", "typecheck"}
	for _, name := range scriptNames {
		if strings.Contains(content, fmt.Sprintf(`"%s"`, name)) {
			switch name {
			case "build":
				scripts["build"] = "npm run build"
			case "test":
				scripts["test"] = "npm test"
			case "lint":
				scripts["lint"] = "npm run lint"
			case "dev":
				scripts["dev"] = "npm run dev"
			case "typecheck":
				scripts["typecheck"] = "npm run typecheck"
			}
		}
	}

	return scripts
}

// extractMakeTargets extracts common targets from Makefile.
func (ig *InitGenerator) extractMakeTargets(content string) map[string]string {
	targets := make(map[string]string)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		for _, target := range []string{"build", "test", "lint", "check", "fmt", "all"} {
			if strings.HasPrefix(line, target+":") {
				targets[target] = target
			}
		}
	}

	return targets
}

// fileExists checks if a file exists relative to cwd.
func (ig *InitGenerator) fileExists(name string) bool {
	_, err := os.Stat(filepath.Join(ig.cwd, name))
	return err == nil
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

// buildPrompt constructs the user prompt from the template and appends
// gathered project context so the LLM has actual file contents to work with.
func (ig *InitGenerator) buildPrompt(ctx *initContext, args string) string {
	prompt := initTemplate

	// Substitute variables
	prompt = strings.ReplaceAll(prompt, "{{ARGS}}", args)
	prompt = strings.ReplaceAll(prompt, "{{PATH}}", ig.cwd)

	// Append gathered context so the single-shot LLM has real project info.
	prompt += "\n\n" + renderContext(ctx)

	return prompt
}

// renderContext serializes the gathered project context into a structured
// text block that is appended to the LLM prompt.
func renderContext(ctx *initContext) string {
	var b strings.Builder

	b.WriteString("## Project Context (pre-gathered)\n\n")
	b.WriteString("The following files have been read for you. Use this context to generate AGENTS.md.\n\n")

	// Project summary.
	b.WriteString("### Project Summary\n")
	if ctx.ProjectName != "" {
		fmt.Fprintf(&b, "- **Name**: %s\n", ctx.ProjectName)
	}
	if len(ctx.Languages) > 0 {
		fmt.Fprintf(&b, "- **Languages**: %s\n", strings.Join(ctx.Languages, ", "))
	}
	if len(ctx.Frameworks) > 0 {
		fmt.Fprintf(&b, "- **Frameworks**: %s\n", strings.Join(ctx.Frameworks, ", "))
	}
	if ctx.BuildCmd != "" {
		fmt.Fprintf(&b, "- **Build**: `%s`\n", ctx.BuildCmd)
	}
	if ctx.TestCmd != "" {
		fmt.Fprintf(&b, "- **Test**: `%s`\n", ctx.TestCmd)
	}
	if ctx.LintCmd != "" {
		fmt.Fprintf(&b, "- **Lint**: `%s`\n", ctx.LintCmd)
	}
	if ctx.TypecheckCmd != "" {
		fmt.Fprintf(&b, "- **Typecheck**: `%s`\n", ctx.TypecheckCmd)
	}
	b.WriteString("\n")

	// File contents — helper to emit a section.
	writeSection := func(title, content string) {
		if content == "" {
			return
		}
		fmt.Fprintf(&b, "### %s\n```\n%s\n```\n\n", title, content)
	}

	writeSection("README", ctx.READMEContent)
	writeSection("go.mod", ctx.GoModContent)
	writeSection("package.json", ctx.PkgJSONContent)
	writeSection("pyproject.toml", ctx.PyProjectContent)
	writeSection("Cargo.toml", ctx.CargoContent)
	writeSection("Dockerfile", ctx.DockerContent)

	// Config files (Makefile, instruction files, etc.)
	for name, content := range ctx.ConfigFiles {
		writeSection(name, content)
	}

	// CI files.
	for name, content := range ctx.CIFiles {
		writeSection(name, content)
	}

	// Entry points — so the LLM understands architecture.
	for name, content := range ctx.EntryPoints {
		writeSection(name, content)
	}

	// Docker Compose files — service topology.
	for name, content := range ctx.ComposeFiles {
		writeSection(name, content)
	}

	// Scripts — operational context.
	if len(ctx.Scripts) > 0 {
		b.WriteString("### Scripts\n")
		b.WriteString("Script headers (first ~10 lines each) for understanding operational commands:\n\n")
		for name, content := range ctx.Scripts {
			writeSection(name, content)
		}
	}

	// Tool version files.
	if ctx.ToolVersions != "" {
		b.WriteString("### Tool Versions\n")
		b.WriteString(ctx.ToolVersions)
	}

	// Sub-directory instruction files — listed so the LLM can delegate to them.
	if len(ctx.SubInstructions) > 0 {
		b.WriteString("### Sub-directory Instruction Files\n")
		b.WriteString("These files contain authoritative guidance for their directories. ")
		b.WriteString("Delegate to them instead of duplicating their content in root AGENTS.md.\n\n")
		for _, rel := range ctx.SubInstructions {
			fmt.Fprintf(&b, "- `%s`\n", rel)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// ContentUnchanged compares existing file content against new LLM output,
// returning true if they are substantively the same (ignoring whitespace
// differences). This lets /init report "already well-structured" instead of
// needlessly rewriting the file.
func ContentUnchanged(existing, generated string) bool {
	return normalizeWS(existing) == normalizeWS(generated)
}

// normalizeWS collapses all runs of whitespace to a single space and trims.
func normalizeWS(s string) string {
	var b strings.Builder
	inWS := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !inWS {
				b.WriteByte(' ')
				inWS = true
			}
		} else {
			b.WriteRune(r)
			inWS = false
		}
	}
	return strings.TrimSpace(b.String())
}

// CleanInitOutput post-processes LLM output for AGENTS.md:
// - strips wrapping code fences (```markdown ... ```)
// - ensures the file starts with "# AGENTS.md"
func CleanInitOutput(text string) string {
	text = strings.TrimSpace(text)

	// Strip outer code fences. LLMs commonly wrap output in ```markdown ... ```
	// even when told not to.
	if (strings.HasPrefix(text, "```markdown") || strings.HasPrefix(text, "```md") || strings.HasPrefix(text, "```")) &&
		strings.HasSuffix(text, "```") {
		// Remove opening fence line.
		if idx := strings.Index(text, "\n"); idx >= 0 {
			text = text[idx+1:]
		}
		// Remove closing fence.
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}

	// Ensure the header is "# AGENTS.md" not "# CLAUDE.md".
	if strings.HasPrefix(text, "# CLAUDE.md") {
		text = "# AGENTS.md" + text[len("# CLAUDE.md"):]
	}

	// Ensure trailing newline.
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	return text
}
