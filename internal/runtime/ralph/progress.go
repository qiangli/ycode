package ralph

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ProgressLog manages an append-only progress file for Ralph iterations.
type ProgressLog struct {
	path string
}

// NewProgressLog creates a progress log at the given path.
func NewProgressLog(path string) *ProgressLog {
	return &ProgressLog{path: path}
}

// Append adds a timestamped entry to the progress log.
func (p *ProgressLog) Append(iteration int, storyID, action, outcome, learnings string) error {
	entry := fmt.Sprintf("\n## Iteration %d — %s\n", iteration, time.Now().Format("2006-01-02 15:04:05"))
	entry += fmt.Sprintf("Story: %s\n", storyID)
	entry += fmt.Sprintf("Action: %s\n", action)
	entry += fmt.Sprintf("Outcome: %s\n", outcome)
	if learnings != "" {
		entry += fmt.Sprintf("Learnings: %s\n", learnings)
	}

	f, err := os.OpenFile(p.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open progress: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}

// Read returns the full progress log content.
func (p *ProgressLog) Read() (string, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// Reset clears the progress log with a header.
func (p *ProgressLog) Reset(header string) error {
	content := "# Progress Log\n\n" + header + "\n"
	return os.WriteFile(p.path, []byte(content), 0o644)
}

// ExtractLearnings returns all "Learnings:" lines from the log.
func (p *ProgressLog) ExtractLearnings() ([]string, error) {
	content, err := p.Read()
	if err != nil {
		return nil, err
	}
	var learnings []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "Learnings: ") {
			learnings = append(learnings, strings.TrimPrefix(line, "Learnings: "))
		}
	}
	return learnings, nil
}
