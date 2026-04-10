package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const starterYcodeJSON = `{
  "permissions": {
    "defaultMode": "dontAsk"
  }
}
`

const gitignoreComment = "# ycode local artifacts"

var gitignoreEntries = []string{".ycode/settings.local.json", ".ycode/sessions/"}

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
}

// Render produces a human-readable summary of the init report.
func (r *InitReport) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Init\n")
	fmt.Fprintf(&b, "  %-16s %s\n", "Project", r.ProjectRoot)
	for _, a := range r.Artifacts {
		fmt.Fprintf(&b, "  %-16s %s\n", a.Name, a.Status.label())
	}
	fmt.Fprintf(&b, "  %-16s %s\n", "Next step", "Review and tailor the generated guidance")
	return b.String()
}

// InitializeRepo creates the standard ycode workspace artifacts in cwd.
// It is idempotent: existing files are never overwritten.
func InitializeRepo(cwd string) (*InitReport, error) {
	var artifacts []InitArtifact

	ycodeDir := filepath.Join(cwd, ".ycode")
	status, err := ensureDir(ycodeDir)
	if err != nil {
		return nil, fmt.Errorf("create .ycode/: %w", err)
	}
	artifacts = append(artifacts, InitArtifact{Name: ".ycode/", Status: status})

	ycodeJSON := filepath.Join(cwd, ".ycode.json")
	status, err = writeFileIfMissing(ycodeJSON, starterYcodeJSON)
	if err != nil {
		return nil, fmt.Errorf("create .ycode.json: %w", err)
	}
	artifacts = append(artifacts, InitArtifact{Name: ".ycode.json", Status: status})

	gitignorePath := filepath.Join(cwd, ".gitignore")
	status, err = ensureGitignoreEntries(gitignorePath)
	if err != nil {
		return nil, fmt.Errorf("update .gitignore: %w", err)
	}
	artifacts = append(artifacts, InitArtifact{Name: ".gitignore", Status: status})

	claudeMD := filepath.Join(cwd, "CLAUDE.md")
	content := RenderInitClaudeMD(cwd)
	status, err = writeFileIfMissing(claudeMD, content)
	if err != nil {
		return nil, fmt.Errorf("create CLAUDE.md: %w", err)
	}
	artifacts = append(artifacts, InitArtifact{Name: "CLAUDE.md", Status: status})

	return &InitReport{ProjectRoot: cwd, Artifacts: artifacts}, nil
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
}

func detectRepo(cwd string) repoDetection {
	pkgJSON, _ := os.ReadFile(filepath.Join(cwd, "package.json"))
	pkgJSONLower := strings.ToLower(string(pkgJSON))

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
		nextjs:      strings.Contains(pkgJSONLower, "\"next\""),
		react:       strings.Contains(pkgJSONLower, "\"react\""),
		vite:        strings.Contains(pkgJSONLower, "\"vite\""),
		nest:        strings.Contains(pkgJSONLower, "@nestjs"),
		srcDir:      dirExists(filepath.Join(cwd, "src")),
		testsDir:    dirExists(filepath.Join(cwd, "tests")),
		rustDir:     dirExists(filepath.Join(cwd, "rust")),
		cmdDir:      dirExists(filepath.Join(cwd, "cmd")),
		internalDir: dirExists(filepath.Join(cwd, "internal")),
	}
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

func verificationLines(cwd string, d *repoDetection) []string {
	var lines []string
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
func RenderInitClaudeMD(cwd string) string {
	d := detectRepo(cwd)
	var lines []string

	lines = append(lines, "# CLAUDE.md", "")
	lines = append(lines, "This file provides guidance to ycode when working with code in this repository.", "")

	langs := detectedLanguages(&d)
	fws := detectedFrameworks(&d)
	lines = append(lines, "## Detected stack")
	if len(langs) == 0 {
		lines = append(lines, "- No specific language markers were detected yet; document the primary language and verification commands once the project structure settles.")
	} else {
		lines = append(lines, fmt.Sprintf("- Languages: %s.", strings.Join(langs, ", ")))
	}
	if len(fws) == 0 {
		lines = append(lines, "- Frameworks: none detected from the supported starter markers.")
	} else {
		lines = append(lines, fmt.Sprintf("- Frameworks/tooling markers: %s.", strings.Join(fws, ", ")))
	}
	lines = append(lines, "")

	if vl := verificationLines(cwd, &d); len(vl) > 0 {
		lines = append(lines, "## Verification")
		lines = append(lines, vl...)
		lines = append(lines, "")
	}

	if sl := repositoryShapeLines(&d); len(sl) > 0 {
		lines = append(lines, "## Repository shape")
		lines = append(lines, sl...)
		lines = append(lines, "")
	}

	if fl := frameworkNotes(&d); len(fl) > 0 {
		lines = append(lines, "## Framework notes")
		lines = append(lines, fl...)
		lines = append(lines, "")
	}

	lines = append(lines, "## Working agreement")
	lines = append(lines, "- Prefer small, reviewable changes and keep generated bootstrap files aligned with actual repo workflows.")
	lines = append(lines, "- Keep shared defaults in `.ycode.json`; reserve `.ycode/settings.local.json` for machine-local overrides.")
	lines = append(lines, "- Do not overwrite existing `CLAUDE.md` content automatically; update it intentionally when repo workflows change.")
	lines = append(lines, "")

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
