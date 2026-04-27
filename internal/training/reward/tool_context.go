package reward

// ToolContext provides reward functions access to the same sandbox
// the agent used during its rollout. This enables verification
// (e.g., running tests, checking file contents) in the same environment.
type ToolContext struct {
	TaskID string

	// RunCommand executes a command in the sandbox and returns stdout + exit code.
	RunCommand func(command string) (output string, exitCode int, err error)

	// ReadFile reads a file from the sandbox filesystem.
	ReadFile func(path string) (string, error)

	// Cleanup releases sandbox resources. Called automatically after reward scoring.
	Cleanup func() error
}

// Terminal runs a command and returns the output.
// Convenience wrapper around RunCommand.
func (tc *ToolContext) Terminal(command string) (string, int, error) {
	if tc.RunCommand == nil {
		return "", 1, nil
	}
	return tc.RunCommand(command)
}

// VerifyFileContent checks if a file contains the expected content.
func (tc *ToolContext) VerifyFileContent(path, expected string) (bool, error) {
	if tc.ReadFile == nil {
		return false, nil
	}
	content, err := tc.ReadFile(path)
	if err != nil {
		return false, err
	}
	return content == expected, nil
}

// Close releases resources. Safe to call multiple times.
func (tc *ToolContext) Close() error {
	if tc.Cleanup != nil {
		return tc.Cleanup()
	}
	return nil
}
