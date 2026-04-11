package prompt

import (
	"strings"
	"testing"
)

func TestPostCompactionRefresh_Empty(t *testing.T) {
	result := PostCompactionRefresh(nil)
	if result != "" {
		t.Error("should return empty for nil context files")
	}

	result = PostCompactionRefresh([]ContextFile{})
	if result != "" {
		t.Error("should return empty for empty context files")
	}
}

func TestPostCompactionRefresh_ExtractsSections(t *testing.T) {
	content := `# CLAUDE.md

## Project
This is a test project.

## Build & Test
` + "```bash" + `
make build
make test
` + "```" + `

## Architecture
Some architecture info.

## Key Design Decisions
- Decision 1
- Decision 2
`

	files := []ContextFile{
		{Path: "/test/CLAUDE.md", Content: content},
	}

	result := PostCompactionRefresh(files)
	if result == "" {
		t.Fatal("should produce refresh content")
	}
	if !strings.Contains(result, "Build & Test") {
		t.Error("should contain Build & Test section")
	}
	if !strings.Contains(result, "make build") {
		t.Error("should contain build command")
	}
	if !strings.Contains(result, "Key Design Decisions") {
		t.Error("should contain Key Design Decisions section")
	}
	if !strings.Contains(result, "Decision 1") {
		t.Error("should contain decision content")
	}
	if !strings.Contains(result, "Critical project context") {
		t.Error("should have refresh header")
	}
}

func TestPostCompactionRefresh_SkipsUnmatchedSections(t *testing.T) {
	content := `# README

## Overview
This is just an overview.
`
	files := []ContextFile{
		{Path: "/test/README.md", Content: content},
	}

	result := PostCompactionRefresh(files)
	if result != "" {
		t.Error("should return empty when no key sections found")
	}
}

func TestPostCompactionRefresh_RespectsBudget(t *testing.T) {
	// Create a CLAUDE.md with a very large Build & Test section.
	bigContent := "## Build & Test\n" + strings.Repeat("x", MaxRefreshBudget+1000)

	files := []ContextFile{
		{Path: "/test/CLAUDE.md", Content: bigContent},
	}

	result := PostCompactionRefresh(files)
	if len(result) > MaxRefreshBudget+200 { // Allow some overhead for headers.
		t.Errorf("result exceeds budget: %d > %d", len(result), MaxRefreshBudget)
	}
}

func TestExtractMarkdownSection(t *testing.T) {
	content := `# Top
## Section A
Content A line 1
Content A line 2

## Section B
Content B

### Subsection B1
Sub content

## Section C
Content C
`

	tests := []struct {
		heading  string
		contains string
		absent   string
	}{
		{"Section A", "Content A line 1", "Content B"},
		{"Section B", "Content B", "Content C"},
		{"Section B", "Sub content", "Content C"},     // Subsection included
		{"Subsection B1", "Sub content", "Content B"}, // Only subsection
		{"Section C", "Content C", "Content A"},
		{"Missing", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.heading, func(t *testing.T) {
			result := extractMarkdownSection(content, tt.heading)
			if tt.contains != "" && !strings.Contains(result, tt.contains) {
				t.Errorf("section %q should contain %q, got %q", tt.heading, tt.contains, result)
			}
			if tt.absent != "" && strings.Contains(result, tt.absent) {
				t.Errorf("section %q should not contain %q, got %q", tt.heading, tt.absent, result)
			}
		})
	}
}
