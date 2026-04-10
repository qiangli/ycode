package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SkillManifest describes a skill's metadata and capabilities.
type SkillManifest struct {
	Name         string   `json:"name" yaml:"name"`
	Description  string   `json:"description" yaml:"description"`
	Triggers     []string `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	Dependencies []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	HasScripts   bool     `json:"has_scripts" yaml:"-"`
	HasResources bool     `json:"has_resources" yaml:"-"`
}

// SkillDir represents a discovered skill directory.
type SkillDir struct {
	Name      string
	Path      string
	Manifest  *SkillManifest
	SkillMD   string // content of SKILL.md
	Scripts   []string
	Resources []string
}

// DiscoverAllSkills finds all skills across search directories.
func DiscoverAllSkills() ([]*SkillDir, error) {
	dirs := skillSearchDirs()
	var skills []*SkillDir
	seen := make(map[string]bool)

	for _, searchDir := range dirs {
		entries, err := os.ReadDir(searchDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			name := entry.Name()
			if seen[name] {
				continue
			}
			seen[name] = true

			skillPath := filepath.Join(searchDir, name)
			skill, err := loadSkillDir(name, skillPath)
			if err != nil {
				continue
			}
			skills = append(skills, skill)
		}
	}

	return skills, nil
}

// loadSkillDir loads a skill from its directory.
func loadSkillDir(name, path string) (*SkillDir, error) {
	skillMDPath := filepath.Join(path, "SKILL.md")
	content, err := os.ReadFile(skillMDPath)
	if err != nil {
		return nil, fmt.Errorf("no SKILL.md in %s", path)
	}

	skill := &SkillDir{
		Name:    name,
		Path:    path,
		SkillMD: string(content),
	}

	// Parse manifest from frontmatter.
	skill.Manifest = parseSkillManifest(string(content))

	// Discover scripts.
	scriptsDir := filepath.Join(path, "scripts")
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				skill.Scripts = append(skill.Scripts, e.Name())
			}
		}
		skill.Manifest.HasScripts = len(skill.Scripts) > 0
	}

	// Discover resources.
	resourcesDir := filepath.Join(path, "resources")
	if entries, err := os.ReadDir(resourcesDir); err == nil {
		for _, e := range entries {
			skill.Resources = append(skill.Resources, e.Name())
		}
		skill.Manifest.HasResources = len(skill.Resources) > 0
	}

	return skill, nil
}

// parseSkillManifest extracts metadata from SKILL.md frontmatter.
func parseSkillManifest(content string) *SkillManifest {
	manifest := &SkillManifest{}

	// Simple YAML frontmatter parser (between --- delimiters).
	if !strings.HasPrefix(content, "---") {
		return manifest
	}

	end := strings.Index(content[3:], "---")
	if end < 0 {
		return manifest
	}

	frontmatter := content[3 : end+3]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			value = strings.Trim(value, "\"'")

			switch key {
			case "name":
				manifest.Name = value
			case "description":
				manifest.Description = value
			}
		}
	}

	if manifest.Name == "" {
		manifest.Name = "unknown"
	}

	return manifest
}

// ExecuteSkillScript runs a script from a skill's scripts/ directory.
func ExecuteSkillScript(skillDir, scriptName string, args []string) (string, error) {
	scriptPath := filepath.Join(skillDir, "scripts", scriptName)

	info, err := os.Stat(scriptPath)
	if err != nil {
		return "", fmt.Errorf("script not found: %s", scriptPath)
	}

	// Check if executable.
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("script is not executable: %s", scriptPath)
	}

	cmd := exec.Command(scriptPath, args...)
	cmd.Dir = skillDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("script failed: %w\nOutput: %s", err, output)
	}
	return string(output), nil
}

// ReadSkillResource reads a file from the skill's resources/ directory.
func ReadSkillResource(skillDir, resourceName string) (string, error) {
	resourcePath := filepath.Join(skillDir, "resources", resourceName)
	data, err := os.ReadFile(resourcePath)
	if err != nil {
		return "", fmt.Errorf("resource not found: %s", resourcePath)
	}
	return string(data), nil
}

// ListSkillResources lists all resources in a skill's resources/ directory.
func ListSkillResources(skillDir string) ([]string, error) {
	resourcesDir := filepath.Join(skillDir, "resources")
	entries, err := os.ReadDir(resourcesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// FormatSkillList formats discovered skills for display.
func FormatSkillList(skills []*SkillDir) string {
	if len(skills) == 0 {
		return "No skills found."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d skill(s):\n\n", len(skills))
	for _, s := range skills {
		desc := s.Manifest.Description
		if desc == "" {
			desc = "(no description)"
		}
		extras := []string{}
		if s.Manifest.HasScripts {
			extras = append(extras, fmt.Sprintf("%d scripts", len(s.Scripts)))
		}
		if s.Manifest.HasResources {
			extras = append(extras, fmt.Sprintf("%d resources", len(s.Resources)))
		}
		extraStr := ""
		if len(extras) > 0 {
			extraStr = " [" + strings.Join(extras, ", ") + "]"
		}
		fmt.Fprintf(&b, "  %s - %s%s\n    %s\n", s.Name, desc, extraStr, s.Path)
	}
	return b.String()
}

// BundledSkillNames lists the bundled skills expected to be available.
var BundledSkillNames = []string{
	"remember",
	"loop",
	"simplify",
	"review",
	"commit",
	"pr",
}

// InstallBundledSkills creates bundled skill directories with SKILL.md files.
func InstallBundledSkills(baseDir string) error {
	for _, name := range BundledSkillNames {
		skillDir := filepath.Join(baseDir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf("create skill dir %s: %w", name, err)
		}

		skillMD := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillMD); err == nil {
			continue // already exists
		}

		content := bundledSkillContent(name)
		if err := os.WriteFile(skillMD, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write skill %s: %w", name, err)
		}

		// Create manifest.json
		manifest := SkillManifest{Name: name, Description: bundledSkillDescription(name)}
		data, _ := json.MarshalIndent(manifest, "", "  ")
		_ = os.WriteFile(filepath.Join(skillDir, "manifest.json"), data, 0o644)
	}
	return nil
}

func bundledSkillDescription(name string) string {
	switch name {
	case "remember":
		return "Save information to persistent memory"
	case "loop":
		return "Run a prompt or slash command on a recurring interval"
	case "simplify":
		return "Review changed code for reuse, quality, and efficiency"
	case "review":
		return "Review code changes for quality, bugs, and security"
	case "commit":
		return "Create a well-formatted git commit"
	case "pr":
		return "Create a pull request with summary and test plan"
	default:
		return ""
	}
}

func bundledSkillContent(name string) string {
	desc := bundledSkillDescription(name)
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n\n%s\n", name, desc, name, desc)
}
