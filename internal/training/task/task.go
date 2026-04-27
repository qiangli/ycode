package task

// Example is a single training example from a task.
type Example struct {
	ID       string            // unique identifier
	Prompt   string            // the task prompt for the agent
	Expected string            // expected answer/output (for evaluation)
	Metadata map[string]string // task-specific metadata
	TestCode string            // optional test code to verify the result
}

// Task is the interface for generating training examples and evaluating completions.
type Task interface {
	// Name returns the task name (e.g., "gsm8k", "humaneval").
	Name() string

	// Len returns the number of examples in the task.
	Len() int

	// GetExample returns the example at the given index.
	GetExample(index int) (*Example, error)

	// Evaluate checks whether a completion correctly answers the example.
	// Returns a score between 0.0 and 1.0.
	Evaluate(example *Example, completion string) (float64, error)
}
