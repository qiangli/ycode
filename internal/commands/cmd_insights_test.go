package commands

import (
	"strings"
	"testing"
)

func TestFormatInsights_Basic(t *testing.T) {
	r := &InsightsReport{
		PeriodDays:          30,
		TotalSessions:       15,
		TotalTokens:         150000,
		TotalCostUSD:        2.5,
		AvgTokensPerSession: 10000,
		MostUsedTools: []ToolUsageStat{
			{Name: "Read", Count: 120},
			{Name: "Edit", Count: 85},
			{Name: "Bash", Count: 60},
		},
		SessionsPerDay: map[string]int{
			"2026-04-25": 3,
			"2026-04-26": 5,
		},
	}

	out := FormatInsights(r)

	for _, want := range []string{
		"last 30 days",
		"Sessions:        15",
		"Total tokens:    150000",
		"$2.5000",
		"Avg tokens/sess: 10000",
		"Top tools:",
		"Read",
		"120 uses",
		"Edit",
		"85 uses",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, out)
		}
	}
}

func TestFormatInsights_ZeroSessions(t *testing.T) {
	r := &InsightsReport{
		PeriodDays:    7,
		TotalSessions: 0,
	}

	out := FormatInsights(r)

	if strings.Contains(out, "Avg tokens/sess") {
		t.Error("should not show avg tokens when no sessions")
	}
	if !strings.Contains(out, "last 7 days") {
		t.Error("missing period header")
	}
}

func TestFormatInsights_NoTools(t *testing.T) {
	r := &InsightsReport{
		PeriodDays:    14,
		TotalSessions: 5,
		TotalTokens:   50000,
	}

	out := FormatInsights(r)

	if strings.Contains(out, "Top tools:") {
		t.Error("should not show tools section when empty")
	}
}

func TestFormatInsights_ManyTools(t *testing.T) {
	tools := make([]ToolUsageStat, 15)
	for i := range tools {
		tools[i] = ToolUsageStat{Name: "tool" + string(rune('A'+i)), Count: 100 - i}
	}

	r := &InsightsReport{
		PeriodDays:    30,
		TotalSessions: 10,
		MostUsedTools: tools,
	}

	out := FormatInsights(r)

	// Should only show top 10.
	if strings.Contains(out, "toolK") {
		t.Error("should cap at 10 tools")
	}
	if !strings.Contains(out, "toolA") {
		t.Error("should show first tool")
	}
}
