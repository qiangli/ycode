package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

// TestE2E_RunTestsTool_ViaRegistry tests the complete tool invocation flow
// through the registry, matching how the conversation runtime calls tools.
func TestE2E_RunTestsTool_ViaRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	// Create a Go project with mixed test results.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module e2etest\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(dir, "lib.go"), []byte(`package e2etest

func Add(a, b int) int { return a + b }
`), 0644)
	os.WriteFile(filepath.Join(dir, "lib_test.go"), []byte(`package e2etest

import "testing"

func TestAdd_Pass(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Error("1+2 should be 3")
	}
}

func TestAdd_Fail(t *testing.T) {
	if Add(1, 2) != 4 {
		t.Error("intentional failure: 1+2 is not 4")
	}
}

func TestAdd_Skip(t *testing.T) {
	t.Skip("skipping for test")
}
`), 0644)

	// Set up registry just like main.go does.
	reg := NewRegistry()
	RegisterBuiltins(reg)
	RegisterTestRunnerHandler(reg, dir)

	reg.SetPermissionResolver(func() permission.Mode {
		return permission.DangerFullAccess
	})

	// Invoke the tool.
	input := json.RawMessage(`{"framework": "go"}`)
	output, err := reg.Invoke(context.Background(), "run_tests", input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	// Parse the structured output.
	var result TestResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse output: %v\nraw: %s", err, output)
	}

	// Validate structure.
	if result.Framework != "go" {
		t.Errorf("framework: got %q, want %q", result.Framework, "go")
	}
	if result.Success {
		t.Error("expected failure (TestAdd_Fail should fail)")
	}
	if result.Passed != 1 {
		t.Errorf("passed: got %d, want 1", result.Passed)
	}
	if result.Failed != 1 {
		t.Errorf("failed: got %d, want 1", result.Failed)
	}
	if result.Skipped != 1 {
		t.Errorf("skipped: got %d, want 1", result.Skipped)
	}
	if result.Total != 3 {
		t.Errorf("total: got %d, want 3", result.Total)
	}
	if result.Duration == "" {
		t.Error("expected non-empty duration")
	}

	// Validate failure details.
	if len(result.Failures) == 0 {
		t.Fatal("expected failure details")
	}
	f := result.Failures[0]
	if f.Name == "" {
		t.Error("failure name should not be empty")
	}
	if f.Message == "" {
		t.Error("failure message should not be empty")
	}
}

// TestE2E_RunTestsTool_AutoDetect tests framework auto-detection.
func TestE2E_RunTestsTool_AutoDetect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module autotest\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(dir, "auto_test.go"), []byte(`package autotest

import "testing"

func TestAuto(t *testing.T) {}
`), 0644)

	reg := NewRegistry()
	RegisterBuiltins(reg)
	RegisterTestRunnerHandler(reg, dir)
	reg.SetPermissionResolver(func() permission.Mode {
		return permission.DangerFullAccess
	})

	// Don't specify framework — should auto-detect as "go".
	input := json.RawMessage(`{}`)
	output, err := reg.Invoke(context.Background(), "run_tests", input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	var result TestResult
	json.Unmarshal([]byte(output), &result)

	if result.Framework != "go" {
		t.Errorf("auto-detected framework: got %q, want %q", result.Framework, "go")
	}
	if !result.Success {
		t.Errorf("expected success: %v", result.Error)
	}
}

// TestE2E_RunTestsTool_NoFramework tests error handling when no framework is found.
func TestE2E_RunTestsTool_NoFramework(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("no tests here"), 0644)

	reg := NewRegistry()
	RegisterBuiltins(reg)
	RegisterTestRunnerHandler(reg, dir)
	reg.SetPermissionResolver(func() permission.Mode {
		return permission.DangerFullAccess
	})

	input := json.RawMessage(`{}`)
	_, err := reg.Invoke(context.Background(), "run_tests", input)
	if err == nil {
		t.Error("expected error when no test framework detected")
	}
}

// TestE2E_RunTestsTool_WithPath tests specifying a subdirectory.
func TestE2E_RunTestsTool_WithPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module pathtest\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(subdir, "sub_test.go"), []byte(`package sub

import "testing"

func TestSub(t *testing.T) {}
`), 0644)

	reg := NewRegistry()
	RegisterBuiltins(reg)
	RegisterTestRunnerHandler(reg, dir)
	reg.SetPermissionResolver(func() permission.Mode {
		return permission.DangerFullAccess
	})

	input := json.RawMessage(`{"framework": "go", "path": "` + dir + `"}`)
	output, err := reg.Invoke(context.Background(), "run_tests", input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	var result TestResult
	json.Unmarshal([]byte(output), &result)

	if !result.Success {
		t.Errorf("expected success: %v", result.Error)
	}
}
