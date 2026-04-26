package eval

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ResponseContains asserts that the agent's response contains a substring.
type ResponseContains struct {
	Substring string
}

func (a ResponseContains) Check(result *RunResult) error {
	if strings.Contains(result.Response, a.Substring) {
		return nil
	}
	return fmt.Errorf("response does not contain %q", a.Substring)
}

func (a ResponseContains) String() string {
	return fmt.Sprintf("ResponseContains(%q)", a.Substring)
}

// ResponseMatches asserts that the agent's response matches a regex.
type ResponseMatches struct {
	Pattern string
	re      *regexp.Regexp
}

func (a *ResponseMatches) Check(result *RunResult) error {
	if a.re == nil {
		var err error
		a.re, err = regexp.Compile(a.Pattern)
		if err != nil {
			return fmt.Errorf("invalid regex %q: %w", a.Pattern, err)
		}
	}
	if a.re.MatchString(result.Response) {
		return nil
	}
	return fmt.Errorf("response does not match pattern %q", a.Pattern)
}

func (a *ResponseMatches) String() string {
	return fmt.Sprintf("ResponseMatches(%q)", a.Pattern)
}

// NoError asserts that the agent did not return an error.
type NoError struct{}

func (a NoError) Check(result *RunResult) error {
	if result.Error != nil {
		return fmt.Errorf("agent returned error: %v", result.Error)
	}
	return nil
}

func (a NoError) String() string { return "NoError" }

// ToolUsed asserts that a specific tool was called at least once.
type ToolUsed struct {
	ToolName string
}

func (a ToolUsed) Check(result *RunResult) error {
	for _, tc := range result.ToolCalls {
		if tc.Name == a.ToolName {
			return nil
		}
	}
	return fmt.Errorf("tool %q was not called", a.ToolName)
}

func (a ToolUsed) String() string {
	return fmt.Sprintf("ToolUsed(%q)", a.ToolName)
}

// ToolNotUsed asserts that a specific tool was NOT called.
type ToolNotUsed struct {
	ToolName string
}

func (a ToolNotUsed) Check(result *RunResult) error {
	for _, tc := range result.ToolCalls {
		if tc.Name == a.ToolName {
			return fmt.Errorf("tool %q was called but should not have been", a.ToolName)
		}
	}
	return nil
}

func (a ToolNotUsed) String() string {
	return fmt.Sprintf("ToolNotUsed(%q)", a.ToolName)
}

// FileExists asserts that a file exists at the given path (relative to WorkDir).
type FileExists struct {
	Path string
}

func (a FileExists) Check(result *RunResult) error {
	fullPath := a.Path
	if result.WorkDir != "" && !strings.HasPrefix(a.Path, "/") {
		fullPath = result.WorkDir + "/" + a.Path
	}
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return fmt.Errorf("file %q does not exist", fullPath)
	}
	return nil
}

func (a FileExists) String() string {
	return fmt.Sprintf("FileExists(%q)", a.Path)
}

// FileContains asserts that a file contains a substring.
type FileContains struct {
	Path      string
	Substring string
}

func (a FileContains) Check(result *RunResult) error {
	fullPath := a.Path
	if result.WorkDir != "" && !strings.HasPrefix(a.Path, "/") {
		fullPath = result.WorkDir + "/" + a.Path
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("cannot read file %q: %w", fullPath, err)
	}
	if !strings.Contains(string(data), a.Substring) {
		return fmt.Errorf("file %q does not contain %q", fullPath, a.Substring)
	}
	return nil
}

func (a FileContains) String() string {
	return fmt.Sprintf("FileContains(%q, %q)", a.Path, a.Substring)
}

// MaxTurns asserts that the agent completed within a maximum number of turns.
type MaxTurns struct {
	Max int
}

func (a MaxTurns) Check(result *RunResult) error {
	if result.Turns > a.Max {
		return fmt.Errorf("agent used %d turns (max %d)", result.Turns, a.Max)
	}
	return nil
}

func (a MaxTurns) String() string {
	return fmt.Sprintf("MaxTurns(%d)", a.Max)
}

// MaxTokens asserts that the agent stayed within a token budget.
type MaxTokens struct {
	Max int
}

func (a MaxTokens) Check(result *RunResult) error {
	total := result.TotalTokens()
	if total > a.Max {
		return fmt.Errorf("agent used %d tokens (max %d)", total, a.Max)
	}
	return nil
}

func (a MaxTokens) String() string {
	return fmt.Sprintf("MaxTokens(%d)", a.Max)
}

// ToolCount asserts that a specific tool was called exactly N times.
type ToolCount struct {
	ToolName string
	Count    int
}

func (a ToolCount) Check(result *RunResult) error {
	count := 0
	for _, tc := range result.ToolCalls {
		if tc.Name == a.ToolName {
			count++
		}
	}
	if count != a.Count {
		return fmt.Errorf("tool %q was called %d times (expected %d)", a.ToolName, count, a.Count)
	}
	return nil
}

func (a ToolCount) String() string {
	return fmt.Sprintf("ToolCount(%q, %d)", a.ToolName, a.Count)
}

// ExpectedToolSequence is a TrajectoryAssertion that checks tool-call
// ordering using the LCS-based trajectory score.
type ExpectedToolSequence struct {
	Expected []string
}

func (a ExpectedToolSequence) Check(toolCalls []ToolCall) (float64, error) {
	actual := make([]string, len(toolCalls))
	for i, tc := range toolCalls {
		actual[i] = tc.Name
	}
	score := TrajectoryLCS(a.Expected, actual)
	if score < 0.5 {
		return score, fmt.Errorf("trajectory score %.2f < 0.5 (expected sequence: %v, actual: %v)",
			score, a.Expected, actual)
	}
	return score, nil
}

func (a ExpectedToolSequence) String() string {
	return fmt.Sprintf("ExpectedToolSequence(%v)", a.Expected)
}

// MinToolAccuracy is a TrajectoryAssertion that checks tool selection
// using the Jaccard similarity metric.
type MinToolAccuracy struct {
	ExpectedTools []string
	MinAccuracy   float64
}

func (a MinToolAccuracy) Check(toolCalls []ToolCall) (float64, error) {
	actual := make([]string, len(toolCalls))
	for i, tc := range toolCalls {
		actual[i] = tc.Name
	}
	accuracy := ToolAccuracy(a.ExpectedTools, actual)
	if accuracy < a.MinAccuracy {
		return accuracy, fmt.Errorf("tool accuracy %.2f < %.2f (expected: %v, actual: %v)",
			accuracy, a.MinAccuracy, a.ExpectedTools, actual)
	}
	return accuracy, nil
}

func (a MinToolAccuracy) String() string {
	return fmt.Sprintf("MinToolAccuracy(%.2f, %v)", a.MinAccuracy, a.ExpectedTools)
}
