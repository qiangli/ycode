package commands

import (
	"fmt"
	"strings"
)

// InsightsReport contains aggregated usage analytics.
type InsightsReport struct {
	PeriodDays          int
	TotalSessions       int
	TotalTokens         int
	TotalCostUSD        float64
	AvgTokensPerSession int
	MostUsedTools       []ToolUsageStat
	SessionsPerDay      map[string]int // date string -> count
}

// ToolUsageStat tracks how often a tool was used.
type ToolUsageStat struct {
	Name  string
	Count int
}

// FormatInsights formats an insights report for display.
func FormatInsights(r *InsightsReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage Insights (last %d days)\n", r.PeriodDays)
	b.WriteString(strings.Repeat("─", 40) + "\n")
	fmt.Fprintf(&b, "Sessions:        %d\n", r.TotalSessions)
	fmt.Fprintf(&b, "Total tokens:    %d\n", r.TotalTokens)
	fmt.Fprintf(&b, "Estimated cost:  $%.4f\n", r.TotalCostUSD)
	if r.TotalSessions > 0 {
		fmt.Fprintf(&b, "Avg tokens/sess: %d\n", r.AvgTokensPerSession)
	}
	if len(r.MostUsedTools) > 0 {
		b.WriteString("\nTop tools:\n")
		for i, t := range r.MostUsedTools {
			if i >= 10 {
				break
			}
			fmt.Fprintf(&b, "  %-20s %d uses\n", t.Name, t.Count)
		}
	}
	return b.String()
}
