package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// testRunnerParams holds the parsed input for the run_tests tool.
type testRunnerParams struct {
	Pattern   string `json:"pattern,omitempty"`
	Path      string `json:"path,omitempty"`
	Framework string `json:"framework,omitempty"`
	Timeout   int    `json:"timeout,omitempty"`
}

// TestResult is the structured output returned by run_tests.
type TestResult struct {
	Framework string        `json:"framework"`
	Passed    int           `json:"passed"`
	Failed    int           `json:"failed"`
	Skipped   int           `json:"skipped"`
	Total     int           `json:"total"`
	Duration  string        `json:"duration"`
	Success   bool          `json:"success"`
	Failures  []TestFailure `json:"failures,omitempty"`
	RawOutput string        `json:"raw_output,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// TestFailure describes a single test failure with location.
type TestFailure struct {
	Name    string `json:"name"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

// RegisterTestRunnerHandler registers the run_tests tool handler.
func RegisterTestRunnerHandler(r *Registry, workDir string) {
	spec, ok := r.Get("run_tests")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params testRunnerParams
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse run_tests input: %w", err)
		}

		dir := params.Path
		if dir == "" {
			dir = workDir
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(workDir, dir)
		}

		timeout := 120
		if params.Timeout > 0 {
			timeout = params.Timeout
		}
		if timeout > 600 {
			timeout = 600
		}

		framework := params.Framework
		if framework == "" || framework == "auto" {
			framework = detectFramework(dir)
		}
		if framework == "" {
			return "", fmt.Errorf("could not detect test framework in %s — specify framework explicitly", dir)
		}

		ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()

		result := runTests(ctx, framework, dir, params.Pattern)

		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal test result: %w", err)
		}
		return string(out), nil
	}
}

// detectFramework identifies the test framework from project files.
func detectFramework(dir string) string {
	checks := []struct {
		file      string
		framework string
	}{
		{"go.mod", "go"},
		{"Cargo.toml", "cargo"},
		{"pyproject.toml", "pytest"},
		{"setup.py", "pytest"},
		{"setup.cfg", "pytest"},
		{"pytest.ini", "pytest"},
		{"jest.config.js", "jest"},
		{"jest.config.ts", "jest"},
		{"jest.config.mjs", "jest"},
		{"vitest.config.ts", "vitest"},
		{"vitest.config.js", "vitest"},
		{"vitest.config.mts", "vitest"},
	}

	// Walk upward to find project root markers.
	current := dir
	for range 10 {
		for _, c := range checks {
			if _, err := os.Stat(filepath.Join(current, c.file)); err == nil {
				return c.framework
			}
		}
		// Check package.json for test scripts (jest/vitest).
		if pkgJSON, err := os.ReadFile(filepath.Join(current, "package.json")); err == nil {
			content := string(pkgJSON)
			if strings.Contains(content, "vitest") {
				return "vitest"
			}
			if strings.Contains(content, "jest") {
				return "jest"
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return ""
}

// runTests executes tests for the given framework and parses the output.
func runTests(ctx context.Context, framework, dir, pattern string) *TestResult {
	switch framework {
	case "go":
		return runGoTests(ctx, dir, pattern)
	case "pytest":
		return runPytestTests(ctx, dir, pattern)
	case "jest":
		return runJestTests(ctx, dir, pattern)
	case "vitest":
		return runVitestTests(ctx, dir, pattern)
	case "cargo":
		return runCargoTests(ctx, dir, pattern)
	default:
		return &TestResult{
			Framework: framework,
			Error:     fmt.Sprintf("unsupported test framework: %s", framework),
		}
	}
}

func runGoTests(ctx context.Context, dir, pattern string) *TestResult {
	args := []string{"test", "-v", "-count=1", "-json"}
	if pattern != "" {
		args = append(args, "-run", pattern)
	}
	args = append(args, "./...")

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &TestResult{
		Framework: "go",
		Duration:  duration.Truncate(time.Millisecond).String(),
	}

	// Parse go test -json output.
	type goTestEvent struct {
		Action  string  `json:"Action"`
		Package string  `json:"Package"`
		Test    string  `json:"Test"`
		Output  string  `json:"Output"`
		Elapsed float64 `json:"Elapsed"`
	}

	failedOutputs := make(map[string][]string) // test name -> output lines

	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev goTestEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Test == "" {
			continue // package-level event
		}
		switch ev.Action {
		case "pass":
			result.Passed++
		case "fail":
			result.Failed++
			fullName := ev.Package + "." + ev.Test
			result.Failures = append(result.Failures, TestFailure{
				Name:    fullName,
				Message: strings.Join(failedOutputs[fullName], ""),
			})
		case "skip":
			result.Skipped++
		case "output":
			fullName := ev.Package + "." + ev.Test
			failedOutputs[fullName] = append(failedOutputs[fullName], ev.Output)
		}
	}

	// Extract file:line from failure output.
	fileLineRe := regexp.MustCompile(`^\s*(\S+\.go):(\d+):`)
	for i, f := range result.Failures {
		for _, line := range strings.Split(f.Message, "\n") {
			if m := fileLineRe.FindStringSubmatch(line); m != nil {
				result.Failures[i].File = m[1]
				result.Failures[i].Line, _ = strconv.Atoi(m[2])
				break
			}
		}
		// Trim output to keep it concise.
		if len(f.Message) > 2000 {
			result.Failures[i].Message = f.Message[:2000] + "\n... (truncated)"
		}
	}

	result.Total = result.Passed + result.Failed + result.Skipped
	result.Success = result.Failed == 0 && err == nil

	if err != nil && result.Total == 0 {
		// Build error or no tests found.
		result.Error = stderr.String()
		if result.Error == "" {
			result.Error = err.Error()
		}
	}

	return result
}

func runPytestTests(ctx context.Context, dir, pattern string) *TestResult {
	args := []string{"-v", "--tb=short", "--no-header", "-q"}
	if pattern != "" {
		args = append(args, "-k", pattern)
	}

	cmd := exec.CommandContext(ctx, "pytest", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &TestResult{
		Framework: "pytest",
		Duration:  duration.Truncate(time.Millisecond).String(),
	}

	output := stdout.String()

	// Parse pytest output.
	// PASSED/FAILED lines: path/test.py::test_name PASSED/FAILED
	passRe := regexp.MustCompile(`(?m)^(.+?::[\w]+)\s+PASSED`)
	failRe := regexp.MustCompile(`(?m)^(.+?::[\w]+)\s+FAILED`)
	skipRe := regexp.MustCompile(`(?m)^(.+?::[\w]+)\s+SKIPPED`)

	result.Passed = len(passRe.FindAllString(output, -1))
	result.Skipped = len(skipRe.FindAllString(output, -1))

	failMatches := failRe.FindAllStringSubmatch(output, -1)
	result.Failed = len(failMatches)

	// Parse failure details from FAILURES section.
	failureSections := strings.Split(output, "FAILED")
	for _, m := range failMatches {
		failure := TestFailure{Name: m[1]}
		// Try to find the failure message in the output.
		for _, section := range failureSections {
			if strings.Contains(section, m[1]) {
				// Extract file:line.
				flRe := regexp.MustCompile(`(\S+\.py):(\d+)`)
				if flm := flRe.FindStringSubmatch(section); flm != nil {
					failure.File = flm[1]
					failure.Line, _ = strconv.Atoi(flm[2])
				}
				// Trim section for message.
				msg := strings.TrimSpace(section)
				if len(msg) > 2000 {
					msg = msg[:2000] + "\n... (truncated)"
				}
				failure.Message = msg
				break
			}
		}
		result.Failures = append(result.Failures, failure)
	}

	// Fallback: parse summary line "N passed, M failed, K skipped"
	summaryRe := regexp.MustCompile(`(\d+)\s+passed`)
	if m := summaryRe.FindStringSubmatch(output); m != nil {
		result.Passed, _ = strconv.Atoi(m[1])
	}
	failedSummaryRe := regexp.MustCompile(`(\d+)\s+failed`)
	if m := failedSummaryRe.FindStringSubmatch(output); m != nil {
		result.Failed, _ = strconv.Atoi(m[1])
	}

	result.Total = result.Passed + result.Failed + result.Skipped
	result.Success = result.Failed == 0 && err == nil

	if err != nil && result.Total == 0 {
		result.Error = stderr.String()
		if result.Error == "" {
			result.Error = err.Error()
		}
	}

	return result
}

func runJestTests(ctx context.Context, dir, pattern string) *TestResult {
	return runNodeTests(ctx, dir, pattern, "jest")
}

func runVitestTests(ctx context.Context, dir, pattern string) *TestResult {
	return runNodeTests(ctx, dir, pattern, "vitest")
}

func runNodeTests(ctx context.Context, dir, pattern, runner string) *TestResult {
	args := []string{runner, "--", "--reporter=verbose"}
	if pattern != "" {
		args = append(args, "-t", pattern)
	}

	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &TestResult{
		Framework: runner,
		Duration:  duration.Truncate(time.Millisecond).String(),
	}

	output := stdout.String() + stderr.String()

	// Parse test results — Jest/Vitest output patterns.
	passRe := regexp.MustCompile(`(?m)✓|✔|PASS`)
	failLineRe := regexp.MustCompile(`(?m)✕|✖|✗|FAIL`)

	result.Passed = len(passRe.FindAllString(output, -1))
	result.Failed = len(failLineRe.FindAllString(output, -1))

	// Parse summary: "Tests: N passed, M failed, K total"
	summaryRe := regexp.MustCompile(`Tests:\s+(?:(\d+)\s+failed,?\s*)?(?:(\d+)\s+skipped,?\s*)?(?:(\d+)\s+passed,?\s*)?(\d+)\s+total`)
	if m := summaryRe.FindStringSubmatch(output); m != nil {
		result.Failed, _ = strconv.Atoi(m[1])
		result.Skipped, _ = strconv.Atoi(m[2])
		result.Passed, _ = strconv.Atoi(m[3])
		result.Total, _ = strconv.Atoi(m[4])
	}

	// Parse failure details.
	failBlockRe := regexp.MustCompile(`(?m)● (.+?)\n([\s\S]*?)(?:\n\n|\z)`)
	for _, m := range failBlockRe.FindAllStringSubmatch(output, -1) {
		failure := TestFailure{Name: m[1]}
		msg := m[2]
		// Extract file:line from stack trace.
		flRe := regexp.MustCompile(`at .*?\((.+?):(\d+):\d+\)`)
		if flm := flRe.FindStringSubmatch(msg); flm != nil {
			failure.File = flm[1]
			failure.Line, _ = strconv.Atoi(flm[2])
		}
		if len(msg) > 2000 {
			msg = msg[:2000] + "\n... (truncated)"
		}
		failure.Message = msg
		result.Failures = append(result.Failures, failure)
	}

	if result.Total == 0 {
		result.Total = result.Passed + result.Failed + result.Skipped
	}
	result.Success = result.Failed == 0 && err == nil

	if err != nil && result.Total == 0 {
		result.Error = output
		if len(result.Error) > 4000 {
			result.Error = result.Error[:4000] + "\n... (truncated)"
		}
	}

	return result
}

func runCargoTests(ctx context.Context, dir, pattern string) *TestResult {
	args := []string{"test"}
	if pattern != "" {
		args = append(args, pattern)
	}
	args = append(args, "--", "--format=terse")

	cmd := exec.CommandContext(ctx, "cargo", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &TestResult{
		Framework: "cargo",
		Duration:  duration.Truncate(time.Millisecond).String(),
	}

	output := stdout.String() + stderr.String()

	// Parse cargo test output.
	// "test result: ok. N passed; M failed; K ignored"
	summaryRe := regexp.MustCompile(`test result: \w+\.\s+(\d+)\s+passed;\s+(\d+)\s+failed;\s+(\d+)\s+ignored`)
	if m := summaryRe.FindStringSubmatch(output); m != nil {
		result.Passed, _ = strconv.Atoi(m[1])
		result.Failed, _ = strconv.Atoi(m[2])
		result.Skipped, _ = strconv.Atoi(m[3])
	}

	// Parse individual failures: "---- test_name stdout ----"
	failBlockRe := regexp.MustCompile(`---- (.+?) stdout ----\n([\s\S]*?)(?:---- |\z)`)
	for _, m := range failBlockRe.FindAllStringSubmatch(output, -1) {
		failure := TestFailure{Name: m[1]}
		msg := m[2]
		// Rust test failures often have "thread 'test_name' panicked at 'message', src/file.rs:line:col"
		flRe := regexp.MustCompile(`panicked at .+?,\s*(.+?):(\d+):\d+`)
		if flm := flRe.FindStringSubmatch(msg); flm != nil {
			failure.File = flm[1]
			failure.Line, _ = strconv.Atoi(flm[2])
		}
		if len(msg) > 2000 {
			msg = msg[:2000] + "\n... (truncated)"
		}
		failure.Message = msg
		result.Failures = append(result.Failures, failure)
	}

	result.Total = result.Passed + result.Failed + result.Skipped
	result.Success = result.Failed == 0 && err == nil

	if err != nil && result.Total == 0 {
		result.Error = output
		if len(result.Error) > 4000 {
			result.Error = result.Error[:4000] + "\n... (truncated)"
		}
	}

	return result
}
