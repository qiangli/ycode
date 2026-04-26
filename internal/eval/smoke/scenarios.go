//go:build eval

package smoke

import (
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/eval"
)

// Scenarios returns all smoke-tier evaluation scenarios.
// Each tests a focused agentic capability with 3 trials for pass@k scoring.
func Scenarios() []*eval.Scenario {
	return []*eval.Scenario{
		arithmeticReasoning(),
		toolSelection(),
		fileCreation(),
		editPrecision(),
		searchThenAct(),
	}
}

// arithmeticReasoning tests basic reasoning — "What is 2+2?"
func arithmeticReasoning() *eval.Scenario {
	return &eval.Scenario{
		Name:        "arithmetic_reasoning",
		Description: "Tests basic reasoning: agent should answer '4' to 2+2",
		Tier:        eval.TierSmoke,
		Policy:      eval.AlwaysPasses,
		Prompt:      "What is 2+2? Reply with just the number.",
		Assertions: []eval.Assertion{
			eval.ResponseContains{Substring: "4"},
			eval.NoError{},
		},
	}
}

// toolSelection tests that the agent correctly uses read_file when asked to read.
func toolSelection() *eval.Scenario {
	return &eval.Scenario{
		Name:        "tool_selection_read",
		Description: "Tests tool selection: agent should use read_file to read a file",
		Tier:        eval.TierSmoke,
		Policy:      eval.UsuallyPasses,
		Prompt:      "Read the file hello.txt in the current directory and tell me what it says.",
		Setup: func(workDir string) (func(), error) {
			err := os.WriteFile(filepath.Join(workDir, "hello.txt"), []byte("Hello from ycode eval!"), 0o644)
			return nil, err
		},
		Assertions: []eval.Assertion{
			eval.NoError{},
			eval.ToolUsed{ToolName: "read_file"},
			eval.ResponseContains{Substring: "Hello from ycode eval"},
		},
	}
}

// fileCreation tests that the agent can create a file that compiles.
func fileCreation() *eval.Scenario {
	return &eval.Scenario{
		Name:        "file_creation_go",
		Description: "Tests file creation: agent should create a valid Go file",
		Tier:        eval.TierSmoke,
		Policy:      eval.UsuallyPasses,
		Prompt:      "Create a file called greeting.go in the current directory that contains a valid Go program that prints 'Hello, World!' to stdout. Use package main with a main function.",
		Assertions: []eval.Assertion{
			eval.NoError{},
			eval.ToolUsed{ToolName: "write_file"},
			eval.FileExists{Path: "greeting.go"},
			eval.FileContains{Path: "greeting.go", Substring: "package main"},
			eval.FileContains{Path: "greeting.go", Substring: "Hello, World!"},
		},
	}
}

// editPrecision tests surgical editing — fix a specific bug without changing other lines.
func editPrecision() *eval.Scenario {
	return &eval.Scenario{
		Name:        "edit_precision",
		Description: "Tests edit precision: fix a specific bug without unintended changes",
		Tier:        eval.TierSmoke,
		Policy:      eval.UsuallyPasses,
		Prompt:      "The file buggy.go has a bug on the line with 'fmt.Println(\"result:\", x+y)' — it should multiply instead of add. Fix ONLY that line using edit_file. Do not change anything else.",
		Setup: func(workDir string) (func(), error) {
			code := `package main

import "fmt"

func calculate(x, y int) {
	fmt.Println("starting calculation")
	fmt.Println("result:", x+y)
	fmt.Println("done")
}

func main() {
	calculate(3, 4)
}
`
			return nil, os.WriteFile(filepath.Join(workDir, "buggy.go"), []byte(code), 0o644)
		},
		Assertions: []eval.Assertion{
			eval.NoError{},
			eval.ToolUsed{ToolName: "edit_file"},
			eval.FileContains{Path: "buggy.go", Substring: "x*y"},
			// Ensure untouched lines are preserved.
			eval.FileContains{Path: "buggy.go", Substring: "starting calculation"},
			eval.FileContains{Path: "buggy.go", Substring: "done"},
		},
	}
}

// searchThenAct tests that the agent uses grep to find files before acting.
func searchThenAct() *eval.Scenario {
	return &eval.Scenario{
		Name:        "search_then_act",
		Description: "Tests search capability: agent should use grep to find TODO comments",
		Tier:        eval.TierSmoke,
		Policy:      eval.UsuallyPasses,
		Prompt:      "Find all Go files in the current directory that contain TODO comments and list them.",
		Setup: func(workDir string) (func(), error) {
			files := map[string]string{
				"main.go":   "package main\n\n// TODO: implement main\nfunc main() {}\n",
				"utils.go":  "package main\n\nfunc helper() string {\n\treturn \"ok\"\n}\n",
				"config.go": "package main\n\n// TODO: add configuration\nvar cfg = struct{}{}\n",
			}
			for name, content := range files {
				if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0o644); err != nil {
					return nil, err
				}
			}
			return nil, nil
		},
		Assertions: []eval.Assertion{
			eval.NoError{},
			eval.ToolUsed{ToolName: "grep_search"},
			eval.ResponseContains{Substring: "main.go"},
			eval.ResponseContains{Substring: "config.go"},
			// utils.go should NOT be mentioned (no TODO).
		},
	}
}
