package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSkill_NotFound(t *testing.T) {
	_, err := discoverSkill("nonexistent-skill-12345")
	if err == nil {
		t.Error("expected error for nonexistent skill")
	}
}

func TestSkillSearchDirs(t *testing.T) {
	dirs := skillSearchDirs()
	if len(dirs) == 0 {
		t.Error("should return at least one search directory")
	}
}

func TestParseSkillManifest(t *testing.T) {
	content := "---\nname: test-skill\ndescription: A test skill\n---\n\n# Test Skill\n\nContent here."
	manifest := parseSkillManifest(content)
	if manifest.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", manifest.Name)
	}
	if manifest.Description != "A test skill" {
		t.Errorf("expected description 'A test skill', got %q", manifest.Description)
	}
}

func TestParseSkillManifest_NoFrontmatter(t *testing.T) {
	content := "# Just markdown\n\nNo frontmatter here."
	manifest := parseSkillManifest(content)
	// Without frontmatter, name defaults to empty string.
	if manifest.Name != "" {
		t.Errorf("expected empty name, got %q", manifest.Name)
	}
}

func TestLoadSkillDir(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "test-skill")
	os.MkdirAll(skillDir, 0o755)

	skillMD := "---\nname: test-skill\ndescription: A test\n---\n\n# Test\nContent."
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644)

	skill, err := loadSkillDir("test-skill", skillDir)
	if err != nil {
		t.Fatalf("load skill: %v", err)
	}
	if skill.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", skill.Name)
	}
	if skill.SkillMD != skillMD {
		t.Error("skill MD content mismatch")
	}
}

func TestInstallBundledSkills(t *testing.T) {
	dir := t.TempDir()
	if err := InstallBundledSkills(dir); err != nil {
		t.Fatalf("install bundled: %v", err)
	}

	for _, name := range BundledSkillNames {
		path := filepath.Join(dir, name, "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("bundled skill %s not installed: %v", name, err)
		}
	}
}

func TestFormatSkillList_Empty(t *testing.T) {
	result := FormatSkillList(nil)
	if result != "No skills found." {
		t.Errorf("unexpected: %q", result)
	}
}
