package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StateSnapshot is a cumulative workspace state that is updated (not appended)
// on each compaction. It represents the integrated state across all compactions.
type StateSnapshot struct {
	PrimaryGoal      string    `json:"primary_goal,omitempty"`
	CompletedSteps   []string  `json:"completed_steps,omitempty"`
	CurrentStep      string    `json:"current_step,omitempty"`
	WorkingFiles     []string  `json:"working_files,omitempty"`
	EnvironmentState string    `json:"environment_state,omitempty"` // e.g., "tests passing", "build broken"
	CompactionCount  int       `json:"compaction_count"`
	LastUpdated      time.Time `json:"last_updated"`
}

// UpdateStateSnapshot merges new compaction information into the cumulative state.
// If existing is nil, creates a new snapshot from the intent summary.
func UpdateStateSnapshot(existing *StateSnapshot, intentSummary string) *StateSnapshot {
	snap := &StateSnapshot{
		LastUpdated: time.Now(),
	}
	if existing != nil {
		*snap = *existing
		snap.CompactionCount++
		snap.LastUpdated = time.Now()

		// Move current step to completed if it exists.
		if snap.CurrentStep != "" {
			snap.CompletedSteps = append(snap.CompletedSteps, snap.CurrentStep)
			// Keep only last 10 completed steps.
			if len(snap.CompletedSteps) > 10 {
				snap.CompletedSteps = snap.CompletedSteps[len(snap.CompletedSteps)-10:]
			}
			snap.CurrentStep = ""
		}
	}

	// Parse structured fields from intent summary.
	if goal := extractIntentField(intentSummary, "Primary Goal:"); goal != "" {
		snap.PrimaryGoal = goal
		snap.CurrentStep = goal
	}

	if files := extractIntentField(intentSummary, "Working Set:"); files != "" {
		snap.WorkingFiles = splitCSV(files)
	}

	// Infer environment state from blockers and verified facts.
	if blockers := extractIntentSection(intentSummary, "Active Blockers:"); len(blockers) > 0 {
		snap.EnvironmentState = "blocked: " + blockers[0]
	} else if facts := extractIntentSection(intentSummary, "Verified Facts:"); len(facts) > 0 {
		for _, f := range facts {
			if strings.Contains(strings.ToLower(f), "test") && strings.Contains(strings.ToLower(f), "pass") {
				snap.EnvironmentState = "tests passing"
				break
			}
			if strings.Contains(strings.ToLower(f), "build") && strings.Contains(strings.ToLower(f), "succeed") {
				snap.EnvironmentState = "build succeeded"
				break
			}
		}
	}

	return snap
}

// Format produces a concise markdown block for injection into the continuation message.
func (s *StateSnapshot) Format() string {
	var lines []string
	lines = append(lines, "<state_snapshot>")

	if s.PrimaryGoal != "" {
		lines = append(lines, "Goal: "+s.PrimaryGoal)
	}
	if len(s.CompletedSteps) > 0 {
		lines = append(lines, "Completed:")
		for _, step := range s.CompletedSteps {
			lines = append(lines, "- "+step)
		}
	}
	if s.CurrentStep != "" {
		lines = append(lines, "Current: "+s.CurrentStep)
	}
	if len(s.WorkingFiles) > 0 {
		lines = append(lines, "Files: "+strings.Join(s.WorkingFiles, ", "))
	}
	if s.EnvironmentState != "" {
		lines = append(lines, "State: "+s.EnvironmentState)
	}
	lines = append(lines, fmt.Sprintf("Compactions: %d", s.CompactionCount))
	lines = append(lines, "</state_snapshot>")

	return strings.Join(lines, "\n")
}

// SaveStateSnapshot persists the state snapshot to disk.
func SaveStateSnapshot(sessionDir string, snap *StateSnapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state snapshot: %w", err)
	}
	path := filepath.Join(sessionDir, "state_snapshot.json")
	return os.WriteFile(path, data, 0o644)
}

// LoadStateSnapshot reads the state snapshot from disk.
// Returns nil, nil if the file doesn't exist.
func LoadStateSnapshot(sessionDir string) (*StateSnapshot, error) {
	path := filepath.Join(sessionDir, "state_snapshot.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state snapshot: %w", err)
	}

	var snap StateSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshal state snapshot: %w", err)
	}
	return &snap, nil
}

// extractIntentField extracts a single-line field value from an intent summary.
func extractIntentField(summary, prefix string) string {
	for _, line := range strings.Split(summary, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		}
	}
	return ""
}

// extractIntentSection extracts bullet items following a section header.
func extractIntentSection(summary, header string) []string {
	var items []string
	inSection := false

	for _, line := range strings.Split(summary, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == header {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(trimmed, "- ") {
				items = append(items, strings.TrimPrefix(trimmed, "- "))
			} else if trimmed != "" {
				break // End of section.
			}
		}
	}
	return items
}

// splitCSV splits a comma-separated string, trimming whitespace.
func splitCSV(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
