package task

import (
	"fmt"
	"strings"
)

// HumanEval implements code generation tasks with test verification.
type HumanEval struct {
	examples []Example
}

// NewHumanEval creates a HumanEval task with sample coding problems.
func NewHumanEval() *HumanEval {
	return &HumanEval{
		examples: []Example{
			{
				ID:       "humaneval-001",
				Prompt:   "Write a function called 'has_close_elements' that takes a list of numbers and a threshold, and returns True if any two numbers in the list are closer to each other than the given threshold.",
				Expected: "True",
				TestCode: `
def has_close_elements(numbers, threshold):
    for i in range(len(numbers)):
        for j in range(i + 1, len(numbers)):
            if abs(numbers[i] - numbers[j]) < threshold:
                return True
    return False
assert has_close_elements([1.0, 2.0, 3.0], 0.5) == False
assert has_close_elements([1.0, 2.8, 3.0, 4.0], 0.3) == True`,
			},
			{
				ID:       "humaneval-002",
				Prompt:   "Write a function called 'separate_paren_groups' that takes a string of nested parentheses and returns a list of the separate balanced groups.",
				Expected: "['(()())', '((()))', '()']",
				TestCode: `
def separate_paren_groups(paren_string):
    result = []
    depth = 0
    current = ""
    for c in paren_string:
        if c == '(':
            depth += 1
            current += c
        elif c == ')':
            depth -= 1
            current += c
            if depth == 0:
                result.append(current)
                current = ""
    return result
assert separate_paren_groups('(()()) ((())) ()') == ['(()())', '((()))', '()']`,
			},
			{
				ID:       "humaneval-003",
				Prompt:   "Write a function called 'truncate_number' that takes a positive floating point number and returns the decimal part (e.g., 3.5 returns 0.5).",
				Expected: "0.5",
				TestCode: `
def truncate_number(number):
    return number % 1.0
assert truncate_number(3.5) == 0.5
assert abs(truncate_number(1.33) - 0.33) < 1e-6`,
			},
		},
	}
}

func (h *HumanEval) Name() string { return "humaneval" }
func (h *HumanEval) Len() int     { return len(h.examples) }

func (h *HumanEval) GetExample(index int) (*Example, error) {
	if index < 0 || index >= len(h.examples) {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, len(h.examples))
	}
	e := h.examples[index]
	return &e, nil
}

func (h *HumanEval) Evaluate(example *Example, completion string) (float64, error) {
	// Check if the completion contains the expected output or function definition.
	if strings.Contains(completion, example.Expected) {
		return 1.0, nil
	}
	// Check if it contains a function definition (partial credit).
	if strings.Contains(completion, "def ") && strings.Contains(completion, "return") {
		return 0.5, nil
	}
	return 0.0, nil
}
