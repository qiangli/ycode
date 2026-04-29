package ralph

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Story represents a single user story in a PRD.
type Story struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptanceCriteria"`
	Priority           int      `json:"priority"` // lower = higher priority
	Passes             bool     `json:"passes"`
	Notes              []string `json:"notes,omitempty"`
}

// PRD is a structured product requirements document.
type PRD struct {
	ProjectName string  `json:"projectName"`
	BranchName  string  `json:"branchName"`
	Feature     string  `json:"feature"`
	Stories     []Story `json:"userStories"`
	path        string
	current     *Story // last story returned by NextStory
}

// LoadPRD reads a prd.json file.
func LoadPRD(path string) (*PRD, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read prd: %w", err)
	}
	var prd PRD
	if err := json.Unmarshal(data, &prd); err != nil {
		return nil, fmt.Errorf("parse prd: %w", err)
	}
	prd.path = path
	return &prd, nil
}

// Save persists the PRD back to disk.
func (p *PRD) Save() error {
	if p.path == "" {
		return fmt.Errorf("no path set for PRD")
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path, data, 0o644)
}

// NextStory returns the highest-priority incomplete story.
// Returns nil if all stories pass.
func (p *PRD) NextStory() *Story {
	// Sort by priority (lower = higher priority).
	sorted := make([]Story, len(p.Stories))
	copy(sorted, p.Stories)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	for i := range sorted {
		if !sorted[i].Passes {
			// Return pointer to the original story.
			for j := range p.Stories {
				if p.Stories[j].ID == sorted[i].ID {
					p.current = &p.Stories[j]
					return p.current
				}
			}
		}
	}
	p.current = nil
	return nil
}

// CurrentStory returns the last story returned by NextStory.
func (p *PRD) CurrentStory() *Story {
	return p.current
}

// UpdateStory marks a story as passing or failing and adds a note.
func (p *PRD) UpdateStory(storyID string, passes bool, note string) error {
	for i := range p.Stories {
		if p.Stories[i].ID == storyID {
			p.Stories[i].Passes = passes
			if note != "" {
				p.Stories[i].Notes = append(p.Stories[i].Notes, note)
			}
			return p.Save()
		}
	}
	return fmt.Errorf("story %q not found", storyID)
}

// AllPass returns true if every story passes.
func (p *PRD) AllPass() bool {
	for _, s := range p.Stories {
		if !s.Passes {
			return false
		}
	}
	return true
}

// Progress returns (completed, total) counts.
func (p *PRD) Progress() (int, int) {
	completed := 0
	for _, s := range p.Stories {
		if s.Passes {
			completed++
		}
	}
	return completed, len(p.Stories)
}
