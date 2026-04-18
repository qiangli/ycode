package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RegisterSkillHandler registers the Skill tool handler.
func RegisterSkillHandler(r *Registry) {
	spec, ok := r.Get("Skill")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Skill string `json:"skill"`
			Args  string `json:"args,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse Skill input: %w", err)
		}

		content, err := discoverSkill(params.Skill)
		if err != nil {
			return "", err
		}
		return content, nil
	}
}

// discoverSkill searches for a skill in the ancestry chain.
func discoverSkill(name string) (string, error) {
	// Normalize name (case-insensitive, handle qualified names).
	name = strings.ToLower(name)
	parts := strings.SplitN(name, ":", 2)
	skillName := parts[len(parts)-1]

	// Search paths: project → home → env vars.
	searchDirs := skillSearchDirs()

	for _, dir := range searchDirs {
		path := filepath.Join(dir, skillName, "SKILL.md")
		content, err := os.ReadFile(path)
		if err == nil {
			return string(content), nil
		}

		// Try direct file match.
		path = filepath.Join(dir, skillName+".md")
		content, err = os.ReadFile(path)
		if err == nil {
			return string(content), nil
		}
	}

	return "", fmt.Errorf("skill %q not found", name)
}

// skillSearchDirs returns directories to search for skills.
func skillSearchDirs() []string {
	var dirs []string

	// Project directory.
	cwd, err := os.Getwd()
	if err == nil {
		dir := cwd
		for {
			dirs = append(dirs, filepath.Join(dir, ".agents", "ycode", "skills"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// Home directory.
	home, err := os.UserHomeDir()
	if err == nil {
		dirs = append(dirs, filepath.Join(home, ".agents", "ycode", "skills"))
	}

	// Environment variable.
	if envDir := os.Getenv("YCODE_SKILLS_DIR"); envDir != "" {
		dirs = append(dirs, envDir)
	}

	return dirs
}
