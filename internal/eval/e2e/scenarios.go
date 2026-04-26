//go:build eval_e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/qiangli/ycode/internal/eval"
)

// Scenarios returns all E2E-tier evaluation scenarios.
// These are full coding tasks with real build/test verification.
func Scenarios() []*eval.Scenario {
	return []*eval.Scenario{
		fixFailingTest(),
		implementFunction(),
		refactorExtract(),
		gitWorkflow(),
		multiFileHandler(),
	}
}

// fixFailingTest seeds a Go project with a failing test and asks the agent to fix it.
func fixFailingTest() *eval.Scenario {
	return &eval.Scenario{
		Name:        "fix_failing_test",
		Description: "Fix a Go function so its test passes",
		Tier:        eval.TierE2E,
		Policy:      eval.UsuallyPasses,
		Prompt:      "The file math.go has a bug that causes math_test.go to fail. Read both files, fix the bug, and verify the test passes by running 'go test'.",
		Setup: func(workDir string) (func(), error) {
			// Initialize Go module.
			cmd := exec.Command("go", "mod", "init", "evaltest")
			cmd.Dir = workDir
			if err := cmd.Run(); err != nil {
				return nil, err
			}

			mathGo := `package evaltest

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a - b // BUG: should be a + b
}

// Multiply returns the product of two integers.
func Multiply(a, b int) int {
	return a * b
}
`
			testGo := `package evaltest

import "testing"

func TestAdd(t *testing.T) {
	got := Add(3, 4)
	if got != 7 {
		t.Errorf("Add(3, 4) = %d, want 7", got)
	}
}

func TestMultiply(t *testing.T) {
	got := Multiply(3, 4)
	if got != 12 {
		t.Errorf("Multiply(3, 4) = %d, want 12", got)
	}
}
`
			if err := os.WriteFile(filepath.Join(workDir, "math.go"), []byte(mathGo), 0o644); err != nil {
				return nil, err
			}
			return nil, os.WriteFile(filepath.Join(workDir, "math_test.go"), []byte(testGo), 0o644)
		},
		Assertions: []eval.Assertion{
			eval.NoError{},
			eval.ToolUsed{ToolName: "read_file"},
			eval.ToolUsed{ToolName: "edit_file"},
			eval.FileContains{Path: "math.go", Substring: "a + b"},
			// Multiply should be untouched.
			eval.FileContains{Path: "math.go", Substring: "a * b"},
		},
	}
}

// implementFunction seeds a Go project with a function signature and test,
// asks the agent to implement the function.
func implementFunction() *eval.Scenario {
	return &eval.Scenario{
		Name:        "implement_function",
		Description: "Implement a function given its signature and test",
		Tier:        eval.TierE2E,
		Policy:      eval.UsuallyPasses,
		Prompt:      "Implement the Reverse function in strings.go so that the test in strings_test.go passes. Run 'go test' to verify.",
		Setup: func(workDir string) (func(), error) {
			cmd := exec.Command("go", "mod", "init", "evaltest")
			cmd.Dir = workDir
			if err := cmd.Run(); err != nil {
				return nil, err
			}

			stringsGo := `package evaltest

// Reverse returns the reverse of a string.
// TODO: implement this function.
func Reverse(s string) string {
	return "" // IMPLEMENT ME
}
`
			testGo := `package evaltest

import "testing"

func TestReverse(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "olleh"},
		{"", ""},
		{"a", "a"},
		{"ab", "ba"},
		{"racecar", "racecar"},
	}
	for _, tt := range tests {
		got := Reverse(tt.input)
		if got != tt.want {
			t.Errorf("Reverse(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
`
			if err := os.WriteFile(filepath.Join(workDir, "strings.go"), []byte(stringsGo), 0o644); err != nil {
				return nil, err
			}
			return nil, os.WriteFile(filepath.Join(workDir, "strings_test.go"), []byte(testGo), 0o644)
		},
		Assertions: []eval.Assertion{
			eval.NoError{},
			eval.FileExists{Path: "strings.go"},
			// Should no longer contain the placeholder.
			eval.FileContains{Path: "strings.go", Substring: "func Reverse"},
		},
	}
}

// refactorExtract tests extracting a helper function from a long function.
func refactorExtract() *eval.Scenario {
	return &eval.Scenario{
		Name:        "refactor_extract",
		Description: "Extract a helper function from a long function",
		Tier:        eval.TierE2E,
		Policy:      eval.UsuallyPasses,
		Prompt:      "The processData function in process.go is too long. Extract the validation logic (lines that check for empty name and negative age) into a separate function called validatePerson. Make sure the code still compiles.",
		Setup: func(workDir string) (func(), error) {
			cmd := exec.Command("go", "mod", "init", "evaltest")
			cmd.Dir = workDir
			if err := cmd.Run(); err != nil {
				return nil, err
			}

			code := `package evaltest

import "fmt"

type Person struct {
	Name string
	Age  int
}

func processData(people []Person) []string {
	var results []string
	for _, p := range people {
		// Validation logic - should be extracted
		if p.Name == "" {
			continue
		}
		if p.Age < 0 {
			continue
		}

		// Processing logic
		result := fmt.Sprintf("%s is %d years old", p.Name, p.Age)
		results = append(results, result)
	}
	return results
}
`
			return nil, os.WriteFile(filepath.Join(workDir, "process.go"), []byte(code), 0o644)
		},
		Assertions: []eval.Assertion{
			eval.NoError{},
			eval.FileContains{Path: "process.go", Substring: "validatePerson"},
			eval.FileContains{Path: "process.go", Substring: "func processData"},
		},
	}
}

// gitWorkflow tests creating a branch, making changes, and committing.
func gitWorkflow() *eval.Scenario {
	return &eval.Scenario{
		Name:        "git_workflow",
		Description: "Create a branch, make a change, and commit",
		Tier:        eval.TierE2E,
		Policy:      eval.UsuallyPasses,
		Prompt:      "Initialize a git repo in the current directory, create a branch called 'feature/greeting', create a file greeting.txt with 'Hello!', and commit it with message 'feat: add greeting'.",
		Assertions: []eval.Assertion{
			eval.NoError{},
			eval.FileExists{Path: "greeting.txt"},
			eval.FileContains{Path: "greeting.txt", Substring: "Hello"},
		},
	}
}

// multiFileHandler tests creating a multi-file Go HTTP handler with tests.
func multiFileHandler() *eval.Scenario {
	return &eval.Scenario{
		Name:        "multi_file_handler",
		Description: "Create a Go HTTP handler with a test file",
		Tier:        eval.TierE2E,
		Policy:      eval.UsuallyPasses,
		Prompt: `Create a simple Go HTTP server in the current directory with:
1. handler.go - a handler function that responds with {"status":"ok"} for GET /health
2. handler_test.go - a test that verifies the /health endpoint returns 200 and the correct JSON

Make sure 'go test' passes.`,
		Setup: func(workDir string) (func(), error) {
			cmd := exec.Command("go", "mod", "init", "evaltest")
			cmd.Dir = workDir
			return nil, cmd.Run()
		},
		Assertions: []eval.Assertion{
			eval.NoError{},
			eval.FileExists{Path: "handler.go"},
			eval.FileExists{Path: "handler_test.go"},
			eval.FileContains{Path: "handler.go", Substring: "health"},
		},
	}
}
