// Package agentsmd provides static quality analysis for AGENTS.md / CLAUDE.md
// instruction files. It checks for stale commands, broken paths, generic
// boilerplate, guardrail density, and contradictions against the Makefile.
//
// This is a contract-tier evaluator: pure Go, no LLM, runs with `go test`.
package agentsmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Report holds the structured output of instruction file analysis.
type Report struct {
	FilePath         string    `json:"file_path"`
	TotalLines       int       `json:"total_lines"`
	Score            float64   `json:"score"`             // 0.0-1.0 composite quality
	CommandDensity   float64   `json:"command_density"`   // runnable code blocks / total lines
	GuardrailDensity float64   `json:"guardrail_density"` // prohibitions / total lines
	BoilerplateRatio float64   `json:"boilerplate_ratio"` // boilerplate lines / total lines
	PathAccuracy     float64   `json:"path_accuracy"`     // valid paths / total path refs
	CommandAccuracy  float64   `json:"command_accuracy"`  // valid commands / total command refs
	Boilerplate      []Finding `json:"boilerplate"`       // generic advice detected
	BrokenPaths      []Finding `json:"broken_paths"`      // referenced paths that don't exist
	BrokenCommands   []Finding `json:"broken_commands"`   // make targets / binaries not found
	Contradictions   []Finding `json:"contradictions"`    // claimed vs actual Makefile targets
	GuardrailLines   []Finding `json:"guardrail_lines"`   // detected guardrail/prohibition lines
	CodeBlocks       int       `json:"code_blocks"`       // total fenced code blocks
	Sections         []string  `json:"sections"`          // markdown section headings found
}

// Finding represents a single analysis finding.
type Finding struct {
	Line   int    `json:"line"`
	Text   string `json:"text"`
	Kind   string `json:"kind"`   // "boilerplate", "broken_path", "broken_command", "contradiction", "guardrail"
	Detail string `json:"detail"` // explanation
}

// Options controls what checks run and provides project context.
type Options struct {
	ProjectRoot      string          // for resolving relative paths
	MakefileTargets  map[string]bool // parsed from Makefile; nil = auto-parse if Makefile exists
	SkipPathCheck    bool            // skip filesystem path verification
	SkipCommandCheck bool            // skip command/target verification
}

// Analyze performs static analysis of an instruction file's content.
func Analyze(content string, opts Options) *Report {
	lines := strings.Split(content, "\n")
	r := &Report{
		FilePath:   opts.ProjectRoot,
		TotalLines: len(lines),
	}

	// Auto-parse Makefile targets if not provided.
	if opts.MakefileTargets == nil && opts.ProjectRoot != "" {
		makePath := filepath.Join(opts.ProjectRoot, "Makefile")
		if targets, err := ParseMakefileTargets(makePath); err == nil {
			opts.MakefileTargets = targets
		}
	}

	// Run all checks.
	r.Boilerplate = detectBoilerplate(lines)
	r.GuardrailLines = detectGuardrails(lines)
	r.Sections = extractSections(lines)
	r.CodeBlocks = countCodeBlocks(lines)

	if !opts.SkipPathCheck && opts.ProjectRoot != "" {
		r.BrokenPaths = checkPathReferences(lines, opts.ProjectRoot)
	}

	if !opts.SkipCommandCheck && opts.MakefileTargets != nil {
		r.BrokenCommands = checkMakeTargets(lines, opts.MakefileTargets)
		r.Contradictions = checkContradictions(content, opts.MakefileTargets)
	}

	// Compute metrics.
	nonBlank := countNonBlank(lines)
	if nonBlank > 0 {
		r.CommandDensity = float64(r.CodeBlocks) / float64(nonBlank)
		r.GuardrailDensity = float64(len(r.GuardrailLines)) / float64(nonBlank)
		r.BoilerplateRatio = float64(len(r.Boilerplate)) / float64(nonBlank)
	}

	totalPaths := len(r.BrokenPaths)
	// We need to count total path refs, not just broken ones.
	if !opts.SkipPathCheck && opts.ProjectRoot != "" {
		allPaths := findPathReferences(lines)
		if len(allPaths) > 0 {
			r.PathAccuracy = 1.0 - float64(totalPaths)/float64(len(allPaths))
		} else {
			r.PathAccuracy = 1.0
		}
	} else {
		r.PathAccuracy = 1.0
	}

	if !opts.SkipCommandCheck && opts.MakefileTargets != nil {
		allMakeRefs := findMakeReferences(lines)
		if len(allMakeRefs) > 0 {
			r.CommandAccuracy = 1.0 - float64(len(r.BrokenCommands))/float64(len(allMakeRefs))
		} else {
			r.CommandAccuracy = 1.0
		}
	} else {
		r.CommandAccuracy = 1.0
	}

	r.Score = ComputeScore(r)
	return r
}

// FormatReport renders a Report as a human-readable markdown string.
func FormatReport(r *Report) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Quality Report: %s\n\n", r.FilePath)
	fmt.Fprintf(&b, "| Metric | Value |\n")
	fmt.Fprintf(&b, "|--------|-------|\n")
	fmt.Fprintf(&b, "| **Composite Score** | **%.1f / 10** |\n", r.Score*10)
	fmt.Fprintf(&b, "| Total Lines | %d |\n", r.TotalLines)
	fmt.Fprintf(&b, "| Code Blocks | %d |\n", r.CodeBlocks)
	fmt.Fprintf(&b, "| Command Density | %.1f%% |\n", r.CommandDensity*100)
	fmt.Fprintf(&b, "| Guardrail Density | %.1f%% |\n", r.GuardrailDensity*100)
	fmt.Fprintf(&b, "| Boilerplate Ratio | %.1f%% |\n", r.BoilerplateRatio*100)
	fmt.Fprintf(&b, "| Path Accuracy | %.0f%% |\n", r.PathAccuracy*100)
	fmt.Fprintf(&b, "| Command Accuracy | %.0f%% |\n", r.CommandAccuracy*100)
	fmt.Fprintf(&b, "| Sections | %d |\n", len(r.Sections))

	writeFindings := func(title string, findings []Finding) {
		if len(findings) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n### %s (%d)\n\n", title, len(findings))
		for _, f := range findings {
			fmt.Fprintf(&b, "- **L%d**: %s — %s\n", f.Line, f.Text, f.Detail)
		}
	}

	writeFindings("Broken Paths", r.BrokenPaths)
	writeFindings("Broken Commands", r.BrokenCommands)
	writeFindings("Contradictions", r.Contradictions)
	writeFindings("Boilerplate", r.Boilerplate)

	return b.String()
}

// --- Boilerplate detection ---

// boilerplatePatterns are full-phrase patterns for generic advice.
var boilerplatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)prefer\s+small[\s,]+reviewable\s+changes`),
	regexp.MustCompile(`(?i)write\s+clean\s+code`),
	regexp.MustCompile(`(?i)follow\s+best\s+practices`),
	regexp.MustCompile(`(?i)keep\s+it\s+simple`),
	regexp.MustCompile(`(?i)use\s+meaningful\s+(variable\s+)?names`),
	regexp.MustCompile(`(?i)write\s+unit\s+tests\s+for\s+all`),
	regexp.MustCompile(`(?i)provide\s+helpful\s+error\s+messages`),
	regexp.MustCompile(`(?i)never\s+include\s+sensitive\s+information`),
	regexp.MustCompile(`(?i)document\s+your\s+code`),
	regexp.MustCompile(`(?i)handle\s+errors\s+gracefully`),
	regexp.MustCompile(`(?i)use\s+descriptive\s+commit\s+messages`),
	regexp.MustCompile(`(?i)follow\s+the\s+single\s+responsibility`),
	regexp.MustCompile(`(?i)don'?t\s+repeat\s+yourself`),
	regexp.MustCompile(`(?i)keep\s+functions\s+short`),
	regexp.MustCompile(`(?i)avoid\s+premature\s+optimization`),
	regexp.MustCompile(`(?i)prefer\s+composition\s+over\s+inheritance`),
}

func detectBoilerplate(lines []string) []Finding {
	var findings []Finding
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, pat := range boilerplatePatterns {
			if pat.MatchString(trimmed) {
				findings = append(findings, Finding{
					Line:   i + 1,
					Text:   truncate(trimmed, 80),
					Kind:   "boilerplate",
					Detail: fmt.Sprintf("matches generic pattern: %s", pat.String()),
				})
				break // one finding per line
			}
		}
	}
	return findings
}

// --- Guardrail detection ---

// guardrailPattern matches lines with concrete prohibitions or requirements.
// Requires a keyword followed by at least one word (the subject).
var guardrailPattern = regexp.MustCompile(
	`(?i)(never|must\s+not|must\s+always|always|do\s+not|don'?t|forbidden|prohibited|required)\s+\S+`,
)

func detectGuardrails(lines []string) []Finding {
	var findings []Finding
	inCodeBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if guardrailPattern.MatchString(trimmed) {
			findings = append(findings, Finding{
				Line:   i + 1,
				Text:   truncate(trimmed, 80),
				Kind:   "guardrail",
				Detail: "prohibition or requirement",
			})
		}
	}
	return findings
}

// --- Path reference checking ---

// pathPattern matches file-path-like references in prose (not inside code blocks).
var pathPattern = regexp.MustCompile(
	`(?:` +
		`(?:internal|cmd|pkg|external|docs|scripts|skills|configs|e2e)/[\w./-]+` + // known dir prefixes
		`|` +
		`\./[\w./-]+` + // relative paths
		`|` +
		`[\w.-]+\.(?:go|md|json|yml|yaml|toml|sh|ts|js|py|rs)` + // files with known extensions
		`)`,
)

func findPathReferences(lines []string) []string {
	seen := make(map[string]bool)
	var paths []string
	inCodeBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}
		matches := pathPattern.FindAllString(line, -1)
		for _, m := range matches {
			// Skip obvious non-paths: manifests, URLs, Go test patterns, placeholders.
			if m == "go.mod" || m == "go.sum" || m == "package.json" ||
				strings.HasPrefix(m, "http") ||
				strings.Contains(m, "/...") || // Go test wildcard
				strings.Contains(m, "/path/to/") || // placeholder
				m == "skill.md" { // generic reference
				continue
			}
			if !seen[m] {
				seen[m] = true
				paths = append(paths, m)
			}
		}
	}
	return paths
}

func checkPathReferences(lines []string, projectRoot string) []Finding {
	var findings []Finding
	paths := findPathReferences(lines)

	for _, ref := range paths {
		// Resolve against project root.
		full := filepath.Join(projectRoot, ref)
		if _, err := os.Stat(full); err != nil {
			// Find the line number.
			lineNum := 0
			for i, line := range lines {
				if strings.Contains(line, ref) {
					lineNum = i + 1
					break
				}
			}
			findings = append(findings, Finding{
				Line:   lineNum,
				Text:   ref,
				Kind:   "broken_path",
				Detail: "file or directory not found",
			})
		}
	}
	return findings
}

// --- Make target checking ---

// makeRefPattern matches `make <target>` references.
var makeRefPattern = regexp.MustCompile(`make\s+([a-zA-Z0-9_-]+)`)

func findMakeReferences(lines []string) []string {
	seen := make(map[string]bool)
	var refs []string
	for _, line := range lines {
		matches := makeRefPattern.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			target := m[1]
			if !seen[target] {
				seen[target] = true
				refs = append(refs, target)
			}
		}
	}
	return refs
}

func checkMakeTargets(lines []string, targets map[string]bool) []Finding {
	var findings []Finding
	seen := make(map[string]bool)

	for i, line := range lines {
		matches := makeRefPattern.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			target := m[1]
			if seen[target] {
				continue
			}
			seen[target] = true
			if !targets[target] {
				findings = append(findings, Finding{
					Line:   i + 1,
					Text:   "make " + target,
					Kind:   "broken_command",
					Detail: fmt.Sprintf("target %q not found in Makefile", target),
				})
			}
		}
	}
	return findings
}

func checkContradictions(content string, targets map[string]bool) []Finding {
	// Check for targets mentioned in Makefile but NOT in the instruction file.
	// Only flag important ones that an agent would need.
	importantTargets := []string{"build", "test", "compile", "install", "deploy", "validate", "clean"}
	var findings []Finding

	for _, target := range importantTargets {
		if targets[target] && !strings.Contains(content, "make "+target) {
			findings = append(findings, Finding{
				Kind:   "contradiction",
				Detail: fmt.Sprintf("Makefile has target %q but instruction file never mentions it", target),
			})
		}
	}
	return findings
}

// --- Section extraction ---

func extractSections(lines []string) []string {
	var sections []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			heading := strings.TrimLeft(trimmed, "# ")
			sections = append(sections, heading)
		}
	}
	return sections
}

// --- Helpers ---

func countCodeBlocks(lines []string) int {
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			count++
		}
	}
	return count / 2 // opening + closing = 1 block
}

func countNonBlank(lines []string) int {
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
