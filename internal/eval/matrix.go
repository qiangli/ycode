package eval

import (
	"fmt"
	"strings"
	"time"
)

// MatrixConfig defines a multi-provider comparison run.
type MatrixConfig struct {
	Providers []ProviderEntry // Providers to compare
	Scenarios []*Scenario     // Scenarios to run
	ReportDir string          // Where to save reports
	Version   string          // Git SHA
}

// ProviderEntry identifies a provider+model combination for matrix runs.
type ProviderEntry struct {
	Name  string // "ollama", "anthropic", "openai"
	Model string // model ID
}

// MatrixResult holds results across all providers for comparison.
type MatrixResult struct {
	Timestamp time.Time                        `json:"timestamp"`
	Version   string                           `json:"version"`
	Entries   map[string]*MatrixProviderResult `json:"entries"` // key: "provider/model"
}

// MatrixProviderResult holds eval results for one provider.
type MatrixProviderResult struct {
	Provider  string           `json:"provider"`
	Model     string           `json:"model"`
	Composite float64          `json:"composite_score"`
	Scenarios []ScenarioResult `json:"scenarios"`
	Duration  time.Duration    `json:"duration"`
}

// FormatMatrix generates a human-readable comparison table.
func FormatMatrix(result *MatrixResult) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Provider Matrix Comparison (%s)\n", result.Timestamp.Format("2006-01-02"))
	fmt.Fprintf(&sb, "Version: %s\n", result.Version)
	sb.WriteString(strings.Repeat("=", 90) + "\n\n")

	// Header.
	providers := make([]string, 0, len(result.Entries))
	for k := range result.Entries {
		providers = append(providers, k)
	}

	fmt.Fprintf(&sb, "%-25s", "Scenario")
	for _, p := range providers {
		fmt.Fprintf(&sb, "  %-15s", truncate(p, 15))
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", 25+17*len(providers)) + "\n")

	// Collect all unique scenario names.
	scenarioNames := make(map[string]bool)
	for _, entry := range result.Entries {
		for _, s := range entry.Scenarios {
			scenarioNames[s.Scenario] = true
		}
	}

	for name := range scenarioNames {
		fmt.Fprintf(&sb, "%-25s", truncate(name, 25))
		for _, p := range providers {
			entry := result.Entries[p]
			found := false
			for _, s := range entry.Scenarios {
				if s.Scenario == name {
					fmt.Fprintf(&sb, "  %.2f/%.2f/%.2f  ",
						s.Metrics.PassAtK, s.Metrics.PassPowK, s.Metrics.Flakiness)
					found = true
					break
				}
			}
			if !found {
				fmt.Fprintf(&sb, "  %-15s", "—")
			}
		}
		sb.WriteString("\n")
	}

	// Summary row.
	sb.WriteString(strings.Repeat("-", 25+17*len(providers)) + "\n")
	fmt.Fprintf(&sb, "%-25s", "COMPOSITE")
	for _, p := range providers {
		entry := result.Entries[p]
		fmt.Fprintf(&sb, "  %-15s", fmt.Sprintf("%.0f/100", entry.Composite*100))
	}
	sb.WriteString("\n")

	// Duration row.
	fmt.Fprintf(&sb, "%-25s", "Duration")
	for _, p := range providers {
		entry := result.Entries[p]
		fmt.Fprintf(&sb, "  %-15s", entry.Duration.Truncate(time.Second).String())
	}
	sb.WriteString("\n")

	// Winner.
	sb.WriteString("\n")
	bestProvider := ""
	bestScore := 0.0
	for p, entry := range result.Entries {
		if entry.Composite > bestScore {
			bestScore = entry.Composite
			bestProvider = p
		}
	}
	if bestProvider != "" {
		fmt.Fprintf(&sb, "Winner: %s (%.0f/100)\n", bestProvider, bestScore*100)
	}

	return sb.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
