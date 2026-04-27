package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFramework_NestedDirectory(t *testing.T) {
	// Create a project structure where go.mod is in the parent.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
	subdir := filepath.Join(dir, "internal", "pkg")
	os.MkdirAll(subdir, 0755)

	// Detection from subdirectory should walk up and find go.mod.
	got := detectFramework(subdir)
	if got != "go" {
		t.Errorf("detectFramework from subdir = %q, want %q", got, "go")
	}
}

func TestDetectFramework_PytestIni(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pytest.ini"), []byte("[pytest]"), 0644)
	got := detectFramework(dir)
	if got != "pytest" {
		t.Errorf("detectFramework with pytest.ini = %q, want %q", got, "pytest")
	}
}

func TestDetectFramework_SetupCfg(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "setup.cfg"), []byte("[tool:pytest]"), 0644)
	got := detectFramework(dir)
	if got != "pytest" {
		t.Errorf("detectFramework with setup.cfg = %q, want %q", got, "pytest")
	}
}

func TestDetectFramework_VitestInPackageJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{
		"scripts": {"test": "vitest run"},
		"devDependencies": {"vitest": "^1.0.0"}
	}`), 0644)
	got := detectFramework(dir)
	if got != "vitest" {
		t.Errorf("detectFramework with vitest in package.json = %q, want %q", got, "vitest")
	}
}

func TestRunGoTests_AllPassing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testproject\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(dir, "pass_test.go"), []byte(`package testproject

import "testing"

func TestA(t *testing.T) {}
func TestB(t *testing.T) {}
func TestC(t *testing.T) {}
`), 0644)

	result := runGoTests(t.Context(), dir, "")

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.Passed != 3 {
		t.Errorf("expected 3 passed, got %d", result.Passed)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", result.Failed)
	}
	if len(result.Failures) != 0 {
		t.Errorf("expected no failures, got %d", len(result.Failures))
	}
}

func TestRunGoTests_WithPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testproject\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(dir, "filter_test.go"), []byte(`package testproject

import "testing"

func TestAlpha(t *testing.T) {}
func TestBeta(t *testing.T) {}
`), 0644)

	result := runGoTests(t.Context(), dir, "TestAlpha")

	if !result.Success {
		t.Errorf("expected success: %s", result.Error)
	}
	if result.Passed != 1 {
		t.Errorf("expected 1 passed (filtered), got %d", result.Passed)
	}
}

func TestRunGoTests_BuildError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testproject\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(dir, "bad_test.go"), []byte(`package testproject

import "testing"

func TestBad(t *testing.T) {
	undefined_function()
}
`), 0644)

	result := runGoTests(t.Context(), dir, "")

	if result.Success {
		t.Error("expected failure for build error")
	}
	if result.Error == "" {
		t.Error("expected non-empty error for build error")
	}
}

func TestRunGoTests_FailureLocation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testproject\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(dir, "loc_test.go"), []byte(`package testproject

import "testing"

func TestWithLocation(t *testing.T) {
	t.Errorf("error at this line")
}
`), 0644)

	result := runGoTests(t.Context(), dir, "")

	if result.Failed != 1 {
		t.Fatalf("expected 1 failure, got %d", result.Failed)
	}
	if len(result.Failures) == 0 {
		t.Fatal("expected failure details")
	}

	f := result.Failures[0]
	if f.File == "" {
		t.Error("expected file path in failure")
	}
	if f.Line == 0 {
		t.Error("expected line number in failure")
	}
}

func TestTestResult_JSONSerialization(t *testing.T) {
	result := &TestResult{
		Framework: "go",
		Passed:    5,
		Failed:    1,
		Skipped:   2,
		Total:     8,
		Duration:  "1.5s",
		Success:   false,
		Failures: []TestFailure{
			{Name: "TestFoo", File: "foo_test.go", Line: 10, Message: "assertion failed"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var parsed TestResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed.Framework != "go" {
		t.Errorf("framework: got %q, want %q", parsed.Framework, "go")
	}
	if parsed.Failed != 1 {
		t.Errorf("failed: got %d, want 1", parsed.Failed)
	}
	if len(parsed.Failures) != 1 {
		t.Fatalf("failures: got %d, want 1", len(parsed.Failures))
	}
	if parsed.Failures[0].Line != 10 {
		t.Errorf("failure line: got %d, want 10", parsed.Failures[0].Line)
	}
}

func TestRunGoTests_Skipped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testproject\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(dir, "skip_test.go"), []byte(`package testproject

import "testing"

func TestSkipped(t *testing.T) {
	t.Skip("intentionally skipped")
}

func TestPassing(t *testing.T) {}
`), 0644)

	result := runGoTests(t.Context(), dir, "")
	if !result.Success {
		t.Errorf("expected success: %v", result.Error)
	}
	if result.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", result.Skipped)
	}
	if result.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", result.Passed)
	}
}
