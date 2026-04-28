// compare.go provides side-by-side comparison of multiple instruction file systems.
package agentsmd

import (
	"fmt"
	"strings"
)

// ComparisonEntry holds analysis results for one tool/project.
type ComparisonEntry struct {
	Name   string  // e.g., "ycode", "opencode", "clawcode"
	Report *Report // analysis results
}

// FormatComparison renders a side-by-side table comparing multiple instruction files.
func FormatComparison(entries []ComparisonEntry) string {
	if len(entries) == 0 {
		return "No entries to compare.\n"
	}

	var b strings.Builder

	// Header.
	b.WriteString("## Instruction File Benchmark\n\n")

	// Metrics table.
	b.WriteString("| Metric |")
	for _, e := range entries {
		fmt.Fprintf(&b, " %s |", e.Name)
	}
	b.WriteString("\n|--------|")
	for range entries {
		b.WriteString("--------|")
	}
	b.WriteString("\n")

	// Rows.
	type row struct {
		label string
		fn    func(r *Report) string
	}
	rows := []row{
		{"**Score (0-10)**", func(r *Report) string { return fmt.Sprintf("**%.1f**", r.Score*10) }},
		{"Total Lines", func(r *Report) string { return fmt.Sprintf("%d", r.TotalLines) }},
		{"Code Blocks", func(r *Report) string { return fmt.Sprintf("%d", r.CodeBlocks) }},
		{"Command Density", func(r *Report) string { return fmt.Sprintf("%.1f%%", r.CommandDensity*100) }},
		{"Guardrail Density", func(r *Report) string { return fmt.Sprintf("%.1f%%", r.GuardrailDensity*100) }},
		{"Boilerplate Ratio", func(r *Report) string { return fmt.Sprintf("%.1f%%", r.BoilerplateRatio*100) }},
		{"Path Accuracy", func(r *Report) string { return fmt.Sprintf("%.0f%%", r.PathAccuracy*100) }},
		{"Command Accuracy", func(r *Report) string { return fmt.Sprintf("%.0f%%", r.CommandAccuracy*100) }},
		{"Guardrail Lines", func(r *Report) string { return fmt.Sprintf("%d", len(r.GuardrailLines)) }},
		{"Boilerplate Lines", func(r *Report) string { return fmt.Sprintf("%d", len(r.Boilerplate)) }},
		{"Broken Paths", func(r *Report) string { return fmt.Sprintf("%d", len(r.BrokenPaths)) }},
		{"Broken Commands", func(r *Report) string { return fmt.Sprintf("%d", len(r.BrokenCommands)) }},
		{"Sections", func(r *Report) string { return fmt.Sprintf("%d", len(r.Sections)) }},
	}

	for _, row := range rows {
		fmt.Fprintf(&b, "| %s |", row.label)
		for _, e := range entries {
			fmt.Fprintf(&b, " %s |", row.fn(e.Report))
		}
		b.WriteString("\n")
	}

	// Per-entry findings summary.
	b.WriteString("\n")
	for _, e := range entries {
		if len(e.Report.Boilerplate) == 0 && len(e.Report.BrokenPaths) == 0 &&
			len(e.Report.BrokenCommands) == 0 && len(e.Report.Contradictions) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s — Findings\n\n", e.Name)
		writeFindings := func(title string, findings []Finding) {
			if len(findings) == 0 {
				return
			}
			fmt.Fprintf(&b, "**%s** (%d):\n", title, len(findings))
			for _, f := range findings {
				if f.Line > 0 {
					fmt.Fprintf(&b, "- L%d: `%s` — %s\n", f.Line, f.Text, f.Detail)
				} else {
					fmt.Fprintf(&b, "- %s\n", f.Detail)
				}
			}
			b.WriteString("\n")
		}
		writeFindings("Broken Paths", e.Report.BrokenPaths)
		writeFindings("Broken Commands", e.Report.BrokenCommands)
		writeFindings("Contradictions", e.Report.Contradictions)
		writeFindings("Boilerplate", e.Report.Boilerplate)
	}

	return b.String()
}
