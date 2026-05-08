package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/builtin"
)

// RegisterSkillHandler registers the Skill and skill_list tool handlers.
func RegisterSkillHandler(r *Registry) {
	registerSkillListHandler(r)

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

		// Check for builtin executor first — runs optimized path directly.
		if executor, ok := builtin.GetSkillExecutor(params.Skill); ok {
			return executor(ctx, params.Args)
		}

		// Fall through to SKILL.md discovery.
		content, err := discoverSkill(params.Skill)
		if err != nil {
			return "", err
		}
		return content, nil
	}
}

// DiscoverSkill is the exported entry point for resolving a skill by name.
// It walks the standard search dirs and returns the first SKILL.md /
// skill.md / <name>.md hit. Used by the agentic Skill tool and by the
// `ycode shell` SkillResolver.
func DiscoverSkill(name string) (string, error) { return discoverSkill(name) }

// LoadSkillFromPath reads a skill file directly from a filesystem path.
// Used by the `@<path>` shell sentinel.
func LoadSkillFromPath(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("load skill %q: %w", path, err)
	}
	return string(content), nil
}

// ListSkills enumerates the names of all skills discoverable from the
// current working directory. Used by tab completion in the shell.
func ListSkills() []string {
	dirs := skillSearchDirs()
	seen := make(map[string]struct{})
	var skills []string

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				skillPath := filepath.Join(dir, name, "SKILL.md")
				if _, err := os.Stat(skillPath); err != nil {
					skillPath = filepath.Join(dir, name, "skill.md")
					if _, err := os.Stat(skillPath); err != nil {
						continue
					}
				}
				if _, ok := seen[name]; !ok {
					seen[name] = struct{}{}
					skills = append(skills, name)
				}
			} else if skillName, ok := strings.CutSuffix(name, ".md"); ok {
				if _, dup := seen[skillName]; !dup {
					seen[skillName] = struct{}{}
					skills = append(skills, skillName)
				}
			}
		}
	}
	return skills
}

// discoverSkill searches for a skill in the ancestry chain.
func discoverSkill(name string) (string, error) {
	// Normalize name (case-insensitive, handle qualified names).
	name = strings.ToLower(name)
	parts := strings.SplitN(name, ":", 2)
	skillName := parts[len(parts)-1]

	// Search paths: project → home → env vars.
	searchDirs := skillSearchDirs()

	// Try multiple filename conventions (SKILL.md, skill.md, <name>.md).
	filenames := []string{
		filepath.Join(skillName, "SKILL.md"),
		filepath.Join(skillName, "skill.md"),
		skillName + ".md",
	}

	for _, dir := range searchDirs {
		for _, fn := range filenames {
			path := filepath.Join(dir, fn)
			content, err := os.ReadFile(path)
			if err == nil {
				return string(content), nil
			}
		}
	}

	return "", fmt.Errorf("skill %q not found", name)
}

// skillSearchDirs returns directories to search for skills.
func skillSearchDirs() []string {
	var dirs []string

	// Project directory — walk up from cwd looking for skill directories.
	cwd, err := os.Getwd()
	if err == nil {
		dir := cwd
		for {
			// Check both the project-local skills/ dir and the .agents/ycode/skills/ dir.
			dirs = append(dirs, filepath.Join(dir, "skills"))
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

// registerSkillListHandler registers the skill_list tool handler.
func registerSkillListHandler(r *Registry) {
	spec, ok := r.Get("skill_list")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Query string `json:"query,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse skill_list input: %w", err)
		}

		dirs := skillSearchDirs()
		var skills []string
		seen := make(map[string]bool)

		for _, dir := range dirs {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				name := entry.Name()
				// Check for skill directories (containing SKILL.md or skill.md).
				if entry.IsDir() {
					skillPath := filepath.Join(dir, name, "SKILL.md")
					if _, err := os.Stat(skillPath); err != nil {
						skillPath = filepath.Join(dir, name, "skill.md")
						if _, err := os.Stat(skillPath); err != nil {
							continue
						}
					}
					if !seen[name] {
						seen[name] = true
						if params.Query == "" || strings.Contains(strings.ToLower(name), strings.ToLower(params.Query)) {
							skills = append(skills, name)
						}
					}
				} else if strings.HasSuffix(name, ".md") {
					// Also check for standalone skill .md files.
					skillName := strings.TrimSuffix(name, ".md")
					if !seen[skillName] {
						seen[skillName] = true
						if params.Query == "" || strings.Contains(strings.ToLower(skillName), strings.ToLower(params.Query)) {
							skills = append(skills, skillName)
						}
					}
				}
			}
		}

		if len(skills) == 0 {
			return "No skills found.", nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Available skills (%d):\n", len(skills))
		for _, s := range skills {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		return b.String(), nil
	}
}
