package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const gitignoreComment = "# ycode local artifacts"

var gitignoreEntries = []string{
	".agents/ycode.json",
	".agents/ycode/settings.local.json",
	".agents/ycode/sessions/",
	".agents/ycode/cache/",
	".agents/ycode/logs/",
}

// InitStatus describes the outcome of creating a single artifact.
type InitStatus int

const (
	InitCreated InitStatus = iota
	InitUpdated
	InitSkipped
)

func (s InitStatus) label() string {
	switch s {
	case InitCreated:
		return "created"
	case InitUpdated:
		return "updated"
	default:
		return "skipped (already exists)"
	}
}

// InitArtifact records what happened to one file/directory during init.
type InitArtifact struct {
	Name   string
	Status InitStatus
}

// InitReport collects the results of workspace initialization.
type InitReport struct {
	ProjectRoot string
	Artifacts   []InitArtifact
	Warnings    []string
	GitStatus   string
}

// Render produces a human-readable summary of the init report.
func (r *InitReport) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Init\n")
	fmt.Fprintf(&b, "  %-16s %s\n", "Project", r.ProjectRoot)
	for _, a := range r.Artifacts {
		fmt.Fprintf(&b, "  %-16s %s\n", a.Name, a.Status.label())
	}
	if r.GitStatus != "" {
		fmt.Fprintf(&b, "  %-16s %s\n", "Git", r.GitStatus)
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintf(&b, "\n  Warnings:\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "    - %s\n", w)
		}
	}
	fmt.Fprintf(&b, "  %-16s %s\n", "Next step", "Review and tailor the generated guidance")
	return b.String()
}

// ProjectMetadata holds detected project information.
type ProjectMetadata struct {
	Name       string            `json:"name,omitempty"`
	Languages  []string          `json:"languages"`
	Frameworks []string          `json:"frameworks,omitempty"`
	BuildCmd   string            `json:"buildCommand,omitempty"`
	TestCmd    string            `json:"testCommand,omitempty"`
	LintCmd    string            `json:"lintCommand,omitempty"`
	PackageMgr string            `json:"packageManager,omitempty"`
	HasGit     bool              `json:"hasGit"`
	Scripts    map[string]string `json:"scripts,omitempty"`
}

// InitializeRepo creates the standard ycode workspace artifacts in cwd.
// It is idempotent: existing files are never overwritten.
func InitializeRepo(cwd string) (*InitReport, error) {
	var artifacts []InitArtifact
	var warnings []string

	// Detect project structure and gather metadata
	detection := detectRepo(cwd)
	metadata := buildProjectMetadata(cwd, &detection)

	// Check git status
	gitStatus := checkGitStatus(cwd)
	if !metadata.HasGit {
		warnings = append(warnings, "No git repository detected. Run 'git init' to initialize version control.")
	}

	// Create .agents/ycode/ directory
	ycodeDir := filepath.Join(cwd, ".agents", "ycode")
	status, err := ensureDir(ycodeDir)
	if err != nil {
		return nil, fmt.Errorf("create .agents/ycode/: %w", err)
	}
	artifacts = append(artifacts, InitArtifact{Name: ".agents/ycode/", Status: status})

	// Create .agents/ycode.json with enhanced content
	ycodeJSON := filepath.Join(cwd, ".agents", "ycode.json")
	status, err = writeYcodeJSON(ycodeJSON, metadata)
	if err != nil {
		return nil, fmt.Errorf("create .agents/ycode.json: %w", err)
	}
	artifacts = append(artifacts, InitArtifact{Name: ".agents/ycode.json", Status: status})

	// Update .gitignore
	gitignorePath := filepath.Join(cwd, ".gitignore")
	status, err = ensureGitignoreEntries(gitignorePath)
	if err != nil {
		return nil, fmt.Errorf("update .gitignore: %w", err)
	}
	artifacts = append(artifacts, InitArtifact{Name: ".gitignore", Status: status})

	// Create CLAUDE.md with enhanced content
	claudeMD := filepath.Join(cwd, "CLAUDE.md")
	content := RenderInitClaudeMD(cwd, metadata)
	status, err = writeFileIfMissing(claudeMD, content)
	if err != nil {
		return nil, fmt.Errorf("create CLAUDE.md: %w", err)
	}
	artifacts = append(artifacts, InitArtifact{Name: "CLAUDE.md", Status: status})

	// Create AGENTS.md (generic AI agent instructions)
	agentsMD := filepath.Join(cwd, "AGENTS.md")
	agentsContent := RenderInitAgentsMD(cwd, metadata)
	status, err = writeFileIfMissing(agentsMD, agentsContent)
	if err != nil {
		return nil, fmt.Errorf("create AGENTS.md: %w", err)
	}
	artifacts = append(artifacts, InitArtifact{Name: "AGENTS.md", Status: status})

	return &InitReport{
		ProjectRoot: cwd,
		Artifacts:   artifacts,
		Warnings:    warnings,
		GitStatus:   gitStatus,
	}, nil
}

func ensureDir(path string) (InitStatus, error) {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return InitSkipped, nil
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return 0, err
	}
	return InitCreated, nil
}

func writeFileIfMissing(path, content string) (InitStatus, error) {
	if _, err := os.Stat(path); err == nil {
		return InitSkipped, nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return 0, err
	}
	return InitCreated, nil
}

func writeYcodeJSON(path string, metadata *ProjectMetadata) (InitStatus, error) {
	if _, err := os.Stat(path); err == nil {
		return InitSkipped, nil
	}

	// Build enhanced config with detected metadata
	config := map[string]interface{}{
		"permissions": map[string]string{
			"defaultMode": "dontAsk",
		},
		"project": map[string]interface{}{
			"languages":  metadata.Languages,
			"frameworks": metadata.Frameworks,
		},
	}

	if metadata.BuildCmd != "" {
		config["build"] = map[string]string{
			"command": metadata.BuildCmd,
		}
	}
	if metadata.TestCmd != "" {
		config["test"] = map[string]string{
			"command": metadata.TestCmd,
		}
	}
	if metadata.LintCmd != "" {
		config["lint"] = map[string]string{
			"command": metadata.LintCmd,
		}
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return 0, err
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return 0, err
	}
	return InitCreated, nil
}

func ensureGitignoreEntries(path string) (InitStatus, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		lines := []string{gitignoreComment}
		lines = append(lines, gitignoreEntries...)
		return InitCreated, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	existing := strings.Split(string(data), "\n")
	changed := false

	hasLine := func(needle string) bool {
		for _, line := range existing {
			if line == needle {
				return true
			}
		}
		return false
	}

	if !hasLine(gitignoreComment) {
		existing = append(existing, gitignoreComment)
		changed = true
	}
	for _, entry := range gitignoreEntries {
		if !hasLine(entry) {
			existing = append(existing, entry)
			changed = true
		}
	}

	if !changed {
		return InitSkipped, nil
	}

	return InitUpdated, os.WriteFile(path, []byte(strings.Join(existing, "\n")+"\n"), 0o644)
}

// checkGitStatus checks if the directory is a git repository.
func checkGitStatus(cwd string) string {
	gitDir := filepath.Join(cwd, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		return "initialized"
	}
	return "not initialized"
}

// repoDetection holds markers found by scanning the project root.
type repoDetection struct {
	goMod         bool
	goWork        bool
	rustWorkspace bool
	rustRoot      bool
	python        bool
	packageJSON   bool
	typescript    bool
	nextjs        bool
	react         bool
	vite          bool
	nest          bool
	srcDir        bool
	testsDir      bool
	rustDir       bool
	cmdDir        bool
	internalDir   bool
	scriptsDir    bool
	// Parsed content
	packageScripts map[string]string
	goModule       string
}

func detectRepo(cwd string) repoDetection {
	pkgJSON, _ := os.ReadFile(filepath.Join(cwd, "package.json"))
	pkgJSONLower := strings.ToLower(string(pkgJSON))

	// Parse package.json for scripts
	var pkgScripts map[string]string
	if len(pkgJSON) > 0 {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(pkgJSON, &pkg); err == nil {
			pkgScripts = pkg.Scripts
		}
	}

	// Parse go.mod for module name
	var goModule string
	if goModContent, err := os.ReadFile(filepath.Join(cwd, "go.mod")); err == nil {
		lines := strings.Split(string(goModContent), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				goModule = strings.TrimSpace(strings.TrimPrefix(line, "module"))
				break
			}
		}
	}

	return repoDetection{
		goMod:         fileExists(filepath.Join(cwd, "go.mod")),
		goWork:        fileExists(filepath.Join(cwd, "go.work")),
		rustWorkspace: fileExists(filepath.Join(cwd, "rust", "Cargo.toml")),
		rustRoot:      fileExists(filepath.Join(cwd, "Cargo.toml")),
		python: fileExists(filepath.Join(cwd, "pyproject.toml")) ||
			fileExists(filepath.Join(cwd, "requirements.txt")) ||
			fileExists(filepath.Join(cwd, "setup.py")),
		packageJSON: fileExists(filepath.Join(cwd, "package.json")),
		typescript: fileExists(filepath.Join(cwd, "tsconfig.json")) ||
			strings.Contains(pkgJSONLower, "typescript"),
		nextjs:         strings.Contains(pkgJSONLower, "\"next\""),
		react:          strings.Contains(pkgJSONLower, "\"react\""),
		vite:           strings.Contains(pkgJSONLower, "\"vite\""),
		nest:           strings.Contains(pkgJSONLower, "@nestjs"),
		srcDir:         dirExists(filepath.Join(cwd, "src")),
		testsDir:       dirExists(filepath.Join(cwd, "tests")),
		rustDir:        dirExists(filepath.Join(cwd, "rust")),
		cmdDir:         dirExists(filepath.Join(cwd, "cmd")),
		internalDir:    dirExists(filepath.Join(cwd, "internal")),
		scriptsDir:     dirExists(filepath.Join(cwd, "scripts")),
		packageScripts: pkgScripts,
		goModule:       goModule,
	}
}

func buildProjectMetadata(cwd string, d *repoDetection) *ProjectMetadata {
	metadata := &ProjectMetadata{
		Languages:  detectedLanguages(d),
		Frameworks: detectedFrameworks(d),
		Scripts:    d.packageScripts,
		HasGit:     dirExists(filepath.Join(cwd, ".git")),
	}

	// Detect package manager
	if d.packageJSON {
		if fileExists(filepath.Join(cwd, "pnpm-lock.yaml")) {
			metadata.PackageMgr = "pnpm"
		} else if fileExists(filepath.Join(cwd, "yarn.lock")) {
			metadata.PackageMgr = "yarn"
		} else if fileExists(filepath.Join(cwd, "package-lock.json")) {
			metadata.PackageMgr = "npm"
		} else if fileExists(filepath.Join(cwd, "bun.lockb")) {
			metadata.PackageMgr = "bun"
		}
	}

	// Detect build command
	metadata.BuildCmd = detectBuildCommand(d, metadata.PackageMgr)
	metadata.TestCmd = detectTestCommand(d, metadata.PackageMgr)
	metadata.LintCmd = detectLintCommand(d, metadata.PackageMgr)

	// Set project name
	if d.goModule != "" {
		metadata.Name = d.goModule
	} else if d.packageJSON {
		metadata.Name = detectProjectName(cwd)
	}

	return metadata
}

func detectProjectName(cwd string) string {
	// Try to get directory name as fallback
	return filepath.Base(cwd)
}

func detectBuildCommand(d *repoDetection, pkgMgr string) string {
	if d.packageJSON && len(d.packageScripts) > 0 {
		// Common build script names
		for _, name := range []string{"build", "compile", "dist", "bundle"} {
			if _, ok := d.packageScripts[name]; ok {
				return fmt.Sprintf("%s run %s", pkgMgrOrDefault(pkgMgr), name)
			}
		}
	}

	if d.goMod {
		return "go build ./..."
	}

	if d.rustWorkspace {
		return "cargo build --workspace"
	}

	if d.rustRoot {
		return "cargo build"
	}

	if d.python {
		if fileExists("pyproject.toml") {
			return "python -m build"
		}
	}

	return ""
}

func detectTestCommand(d *repoDetection, pkgMgr string) string {
	if d.packageJSON && len(d.packageScripts) > 0 {
		for _, name := range []string{"test", "test:unit", "test:ci", "jest", "vitest"} {
			if _, ok := d.packageScripts[name]; ok {
				return fmt.Sprintf("%s run %s", pkgMgrOrDefault(pkgMgr), name)
			}
		}
	}

	if d.goMod {
		return "go test -race ./..."
	}

	if d.rustWorkspace || d.rustRoot {
		return "cargo test --workspace"
	}

	if d.python {
		return "pytest"
	}

	return ""
}

func detectLintCommand(d *repoDetection, pkgMgr string) string {
	if d.packageJSON && len(d.packageScripts) > 0 {
		for _, name := range []string{"lint", "lint:check", "eslint", "prettier:check"} {
			if _, ok := d.packageScripts[name]; ok {
				return fmt.Sprintf("%s run %s", pkgMgrOrDefault(pkgMgr), name)
			}
		}
	}

	if d.goMod {
		return "go vet ./..."
	}

	if d.rustWorkspace || d.rustRoot {
		return "cargo clippy --all-targets"
	}

	if d.python {
		return "ruff check ."
	}

	return ""
}

func pkgMgrOrDefault(pkgMgr string) string {
	if pkgMgr == "" {
		return "npm"
	}
	return pkgMgr
}

func detectedLanguages(d *repoDetection) []string {
	var langs []string
	if d.goMod || d.goWork {
		langs = append(langs, "Go")
	}
	if d.rustWorkspace || d.rustRoot {
		langs = append(langs, "Rust")
	}
	if d.python {
		langs = append(langs, "Python")
	}
	if d.typescript {
		langs = append(langs, "TypeScript")
	} else if d.packageJSON {
		langs = append(langs, "JavaScript/Node.js")
	}
	return langs
}

func detectedFrameworks(d *repoDetection) []string {
	var fws []string
	if d.nextjs {
		fws = append(fws, "Next.js")
	}
	if d.react {
		fws = append(fws, "React")
	}
	if d.vite {
		fws = append(fws, "Vite")
	}
	if d.nest {
		fws = append(fws, "NestJS")
	}
	return fws
}

func verificationLines(cwd string, d *repoDetection, metadata *ProjectMetadata) []string {
	var lines []string

	// Use detected commands from metadata if available
	if metadata.BuildCmd != "" {
		lines = append(lines, fmt.Sprintf("- Build: `%s`", metadata.BuildCmd))
	}
	if metadata.TestCmd != "" {
		lines = append(lines, fmt.Sprintf("- Test: `%s`", metadata.TestCmd))
	}
	if metadata.LintCmd != "" {
		lines = append(lines, fmt.Sprintf("- Lint: `%s`", metadata.LintCmd))
	}

	// Add language-specific verification if no commands detected
	if len(lines) == 0 {
		if d.goMod || d.goWork {
			lines = append(lines, "- Run Go verification: `go build ./...`, `go test -race ./...`, `go vet ./...`")
		}
		if d.rustWorkspace {
			lines = append(lines, "- Run Rust verification from `rust/`: `cargo fmt`, `cargo clippy --workspace --all-targets -- -D warnings`, `cargo test --workspace`")
		} else if d.rustRoot {
			lines = append(lines, "- Run Rust verification from the repo root: `cargo fmt`, `cargo clippy --workspace --all-targets -- -D warnings`, `cargo test --workspace`")
		}
		if d.python {
			if fileExists(filepath.Join(cwd, "pyproject.toml")) {
				lines = append(lines, "- Run the Python project checks declared in `pyproject.toml` (for example: `pytest`, `ruff check`, and `mypy` when configured).")
			} else {
				lines = append(lines, "- Run the repo's Python test/lint commands before shipping changes.")
			}
		}
		if d.packageJSON {
			lines = append(lines, "- Run the JavaScript/TypeScript checks from `package.json` before shipping changes (`npm test`, `npm run lint`, `npm run build`, or the repo equivalent).")
		}
	}

	if d.testsDir && d.srcDir {
		lines = append(lines, "- `src/` and `tests/` are both present; update both surfaces together when behavior changes.")
	}

	return lines
}

func repositoryShapeLines(d *repoDetection) []string {
	var lines []string
	if d.cmdDir {
		lines = append(lines, "- `cmd/` contains main entry points.")
	}
	if d.internalDir {
		lines = append(lines, "- `internal/` contains private packages.")
	}
	if d.scriptsDir {
		lines = append(lines, "- `scripts/` contains build/deployment scripts.")
	}
	if d.rustDir {
		lines = append(lines, "- `rust/` contains the Rust workspace and active CLI/runtime implementation.")
	}
	if d.srcDir {
		lines = append(lines, "- `src/` contains source files that should stay consistent with generated guidance and tests.")
	}
	if d.testsDir {
		lines = append(lines, "- `tests/` contains validation surfaces that should be reviewed alongside code changes.")
	}
	return lines
}

func frameworkNotes(d *repoDetection) []string {
	var lines []string
	if d.nextjs {
		lines = append(lines, "- Next.js detected: preserve routing/data-fetching conventions and verify production builds after changing app structure.")
	}
	if d.react && !d.nextjs {
		lines = append(lines, "- React detected: keep component behavior covered with focused tests and avoid unnecessary prop/API churn.")
	}
	if d.vite {
		lines = append(lines, "- Vite detected: validate the production bundle after changing build-sensitive configuration or imports.")
	}
	if d.nest {
		lines = append(lines, "- NestJS detected: keep module/provider boundaries explicit and verify controller/service wiring after refactors.")
	}
	return lines
}

// RenderInitClaudeMD generates a starter CLAUDE.md based on project detection.
func RenderInitClaudeMD(cwd string, metadata *ProjectMetadata) string {
	d := detectRepo(cwd)
	var lines []string

	lines = append(lines, "# CLAUDE.md", "")

	// Project name
	if metadata.Name != "" && metadata.Name != filepath.Base(cwd) {
		lines = append(lines, metadata.Name, "")
	}

	lines = append(lines, "This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.", "")

	lines = append(lines, "## Detected Stack")
	if len(metadata.Languages) == 0 {
		lines = append(lines, "- No specific language markers were detected yet; document the primary language and verification commands once the project structure settles.")
	} else {
		lines = append(lines, fmt.Sprintf("- **Languages**: %s.", strings.Join(metadata.Languages, ", ")))
	}
	if len(metadata.Frameworks) == 0 {
		lines = append(lines, "- **Frameworks**: none detected from the supported starter markers.")
	} else {
		lines = append(lines, fmt.Sprintf("- **Frameworks**: %s.", strings.Join(metadata.Frameworks, ", ")))
	}
	if metadata.PackageMgr != "" {
		lines = append(lines, fmt.Sprintf("- **Package Manager**: %s.", metadata.PackageMgr))
	}
	lines = append(lines, "")

	if vl := verificationLines(cwd, &d, metadata); len(vl) > 0 {
		lines = append(lines, "## Verification")
		lines = append(lines, "Always run these commands before committing changes:")
		lines = append(lines, "")
		lines = append(lines, vl...)
		lines = append(lines, "")
	}

	if sl := repositoryShapeLines(&d); len(sl) > 0 {
		lines = append(lines, "## Repository Structure")
		lines = append(lines, sl...)
		lines = append(lines, "")
	}

	if fl := frameworkNotes(&d); len(fl) > 0 {
		lines = append(lines, "## Framework Notes")
		lines = append(lines, fl...)
		lines = append(lines, "")
	}

	lines = append(lines, "## Working Conventions")
	lines = append(lines, "- Prefer small, reviewable changes and keep generated bootstrap files aligned with actual repo workflows.")
	lines = append(lines, "- Keep shared defaults in `.agents/ycode.json`; reserve `.agents/ycode/settings.local.json` for machine-local overrides.")
	lines = append(lines, "- Do not overwrite existing `CLAUDE.md` content automatically; update it intentionally when repo workflows change.")
	lines = append(lines, "")

	return strings.Join(lines, "\n")
}

// RenderInitAgentsMD generates a minimal AGENTS.md with references to USAGE.md
// for detailed instructions. If CLAUDE.md exists, a reference is included so
// ycode (and other tools) can follow its instructions too.
func RenderInitAgentsMD(cwd string, metadata *ProjectMetadata) string {
	var lines []string

	lines = append(lines, "# AGENTS.md", "")

	lines = append(lines, "Instructions for AI coding assistants working in this repository.", "")

	// Reference CLAUDE.md if it exists (Claude Code compatibility).
	if fileExists(filepath.Join(cwd, "CLAUDE.md")) {
		lines = append(lines, "**Read [CLAUDE.md](./CLAUDE.md)** for additional project conventions and Claude Code-specific guidance.", "")
	}

	lines = append(lines, "**Read [USAGE.md](./USAGE.md)** for detailed instructions on build commands, configuration, tools, and workflows.", "")

	return strings.Join(lines, "\n")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
