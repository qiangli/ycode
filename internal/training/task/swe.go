package task

import (
	"fmt"
	"strings"
)

// SWETask implements SWE-bench style coding tasks with test verification.
type SWETask struct {
	examples []Example
}

// NewSWETask creates SWE-bench style tasks.
func NewSWETask() *SWETask {
	return &SWETask{
		examples: []Example{
			{
				ID:       "swe-001",
				Prompt:   "Fix the bug in this function that should return the sum of even numbers in a list:\n\ndef sum_evens(nums):\n    total = 0\n    for n in nums:\n        if n % 2 == 0:\n            total += 1  # BUG: should be += n\n    return total",
				Expected: "total += n",
				TestCode: "assert sum_evens([1, 2, 3, 4]) == 6\nassert sum_evens([]) == 0\nassert sum_evens([1, 3, 5]) == 0",
			},
			{
				ID:       "swe-002",
				Prompt:   "Fix the off-by-one error in this binary search:\n\ndef binary_search(arr, target):\n    lo, hi = 0, len(arr)\n    while lo < hi:\n        mid = (lo + hi) // 2\n        if arr[mid] == target:\n            return mid\n        elif arr[mid] < target:\n            lo = mid  # BUG: should be mid + 1\n        else:\n            hi = mid\n    return -1",
				Expected: "lo = mid + 1",
				TestCode: "assert binary_search([1, 2, 3, 4, 5], 3) == 2\nassert binary_search([1, 2, 3, 4, 5], 6) == -1",
			},
		},
	}
}

func (s *SWETask) Name() string { return "swe" }
func (s *SWETask) Len() int     { return len(s.examples) }

func (s *SWETask) GetExample(index int) (*Example, error) {
	if index < 0 || index >= len(s.examples) {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, len(s.examples))
	}
	e := s.examples[index]
	return &e, nil
}

func (s *SWETask) Evaluate(example *Example, completion string) (float64, error) {
	if strings.Contains(completion, example.Expected) {
		return 1.0, nil
	}
	return 0.0, nil
}
