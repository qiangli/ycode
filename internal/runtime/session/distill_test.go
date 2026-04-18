package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDistillToolOutput_SmallOutput(t *testing.T) {
	cfg := DefaultDistillConfig()
	output := "small output"
	result := DistillToolOutput("bash", output, cfg)
	if result != output {
		t.Error("small output should be unchanged")
	}
}

func TestDistillToolOutput_ExemptTool(t *testing.T) {
	cfg := DefaultDistillConfig()
	cfg.ExemptTools = []string{"custom_tool"}
	// Create a large output.
	output := strings.Repeat("x\n", 1000)
	result := DistillToolOutput("custom_tool", output, cfg)
	if result != output {
		t.Error("explicitly exempt tool output should be unchanged regardless of size")
	}
}

func TestDistillToolOutput_LargeOutput(t *testing.T) {
	cfg := DefaultDistillConfig()
	cfg.MaxInlineChars = 100

	// Create output with 50 lines.
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "line "+string(rune('A'+i%26)))
	}
	output := strings.Join(lines, "\n")

	result := DistillToolOutput("bash", output, cfg)

	if !strings.Contains(result, "lines omitted") {
		t.Error("distilled output should contain omission notice")
	}
	// Should contain head lines.
	if !strings.Contains(result, "line A") {
		t.Error("should contain first line")
	}
}

func TestDistillToolOutput_SavesToDisk(t *testing.T) {
	dir := t.TempDir()
	cfg := DistillConfig{
		MaxInlineChars: 100,
		FullOutputDir:  dir,
	}

	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "line "+string(rune('0'+i%10)))
	}
	output := strings.Join(lines, "\n")

	result := DistillToolOutput("grep_search", output, cfg)

	// Should reference saved file.
	if !strings.Contains(result, "full output saved to") {
		t.Error("should mention saved file path")
	}

	// Verify file exists.
	entries, _ := os.ReadDir(dir)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "grep_search_") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected saved output file in dir")
	}
}

func TestDistillToolOutput_NoSaveWithoutDir(t *testing.T) {
	cfg := DistillConfig{
		MaxInlineChars: 100,
		FullOutputDir:  "", // no dir
	}

	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "line "+string(rune('0'+i%10)))
	}
	output := strings.Join(lines, "\n")

	result := DistillToolOutput("bash", output, cfg)

	if strings.Contains(result, "saved to") {
		t.Error("should not mention saved file when dir is empty")
	}
	if !strings.Contains(result, "lines omitted") {
		t.Error("should still contain omission notice")
	}
}

func TestDistillToolOutput_FewLinesButLargeContent(t *testing.T) {
	cfg := DistillConfig{
		MaxInlineChars: 100,
	}

	// 5 very long lines.
	output := strings.Repeat("x", 500) + "\n" + strings.Repeat("y", 500)

	result := DistillToolOutput("bash", output, cfg)

	// Should be truncated by chars, not lines.
	if len(result) > 200 {
		t.Errorf("expected truncated output, got %d chars", len(result))
	}
	if !strings.Contains(result, "truncated") {
		t.Error("should contain truncation notice")
	}
}

func TestAggressiveDistillConfig(t *testing.T) {
	normal := DefaultDistillConfig()
	aggressive := AggressiveDistillConfig()

	if aggressive.MaxInlineChars >= normal.MaxInlineChars {
		t.Errorf("aggressive MaxInlineChars (%d) should be less than normal (%d)",
			aggressive.MaxInlineChars, normal.MaxInlineChars)
	}
	if aggressive.MaxInlineBytes >= normal.MaxInlineBytes {
		t.Errorf("aggressive MaxInlineBytes (%d) should be less than normal (%d)",
			aggressive.MaxInlineBytes, normal.MaxInlineBytes)
	}
	if !aggressive.AggressiveMode {
		t.Error("aggressive config should have AggressiveMode=true")
	}
}

func TestDistillToolOutput_AggressiveHeadTail(t *testing.T) {
	// Create output with enough lines to trigger head+tail truncation.
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", 50)
	}
	output := strings.Join(lines, "\n")

	normalCfg := DefaultDistillConfig()
	normalCfg.MaxInlineChars = 500
	normalResult := DistillToolOutput("bash", output, normalCfg)

	aggCfg := AggressiveDistillConfig()
	aggCfg.MaxInlineChars = 500
	aggResult := DistillToolOutput("bash", output, aggCfg)

	// Aggressive should be shorter (fewer head+tail lines).
	if len(aggResult) >= len(normalResult) {
		t.Errorf("aggressive result (%d chars) should be shorter than normal (%d chars)",
			len(aggResult), len(normalResult))
	}
}

func TestDistillToolOutput_NoDefaultExemptions(t *testing.T) {
	cfg := DefaultDistillConfig()

	// No tools are exempt from distillation by default.
	if len(cfg.ExemptTools) != 0 {
		t.Errorf("expected no exempt tools, got %v", cfg.ExemptTools)
	}

	dir := t.TempDir()
	cfg.FullOutputDir = dir

	bigOutput := strings.Repeat("line\n", 500)

	// read_file should now be distilled like all other tools.
	result := DistillToolOutput("read_file", bigOutput, cfg)
	if result == bigOutput {
		t.Error("read_file should be distilled (no longer exempt)")
	}

	// bash should also be distilled.
	result = DistillToolOutput("bash", bigOutput, cfg)
	if result == bigOutput {
		t.Error("bash should be distilled")
	}

	entries, _ := os.ReadDir(filepath.Join(dir))
	if len(entries) == 0 {
		t.Error("expected saved output file")
	}
}
