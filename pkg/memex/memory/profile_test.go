package memory

import (
	"strings"
	"testing"
)

func TestUserProfile_UpdateAndGet(t *testing.T) {
	p := NewUserProfile()

	p.Update("basic_info.name", "Alice")
	p.Update("preferences.editor", "vim")
	p.Update("expertise", "Go programming")
	p.Update("work_patterns", "TDD workflow")

	if got := p.Get("basic_info.name"); got != "Alice" {
		t.Errorf("name = %q, want Alice", got)
	}
	if got := p.Get("preferences.editor"); got != "vim" {
		t.Errorf("editor = %q, want vim", got)
	}
	if len(p.Expertise) != 1 || p.Expertise[0] != "Go programming" {
		t.Errorf("expertise = %v, want [Go programming]", p.Expertise)
	}
	if len(p.WorkPatterns) != 1 || p.WorkPatterns[0] != "TDD workflow" {
		t.Errorf("work_patterns = %v, want [TDD workflow]", p.WorkPatterns)
	}
}

func TestUserProfile_UpdateDefaultsToBasicInfo(t *testing.T) {
	p := NewUserProfile()
	p.Update("name", "Bob") // no section prefix
	if got := p.Get("name"); got != "Bob" {
		t.Errorf("name = %q, want Bob", got)
	}
}

func TestUserProfile_ExpertiseDedup(t *testing.T) {
	p := NewUserProfile()
	p.Update("expertise", "Go")
	p.Update("expertise", "Go") // duplicate
	p.Update("expertise", "Rust")

	if len(p.Expertise) != 2 {
		t.Errorf("expertise count = %d, want 2", len(p.Expertise))
	}
}

func TestUserProfile_FormatAndParse(t *testing.T) {
	original := NewUserProfile()
	original.BasicInfo["name"] = "Alice"
	original.BasicInfo["role"] = "engineer"
	original.Preferences["editor"] = "vim"
	original.Expertise = []string{"Go", "Rust"}
	original.WorkPatterns = []string{"TDD", "code review"}

	formatted := original.Format()

	// Parse it back.
	parsed := parseProfile(formatted)

	if parsed.BasicInfo["name"] != "Alice" {
		t.Errorf("roundtrip name = %q, want Alice", parsed.BasicInfo["name"])
	}
	if parsed.BasicInfo["role"] != "engineer" {
		t.Errorf("roundtrip role = %q, want engineer", parsed.BasicInfo["role"])
	}
	if parsed.Preferences["editor"] != "vim" {
		t.Errorf("roundtrip editor = %q, want vim", parsed.Preferences["editor"])
	}
	if len(parsed.Expertise) != 2 {
		t.Errorf("roundtrip expertise count = %d, want 2", len(parsed.Expertise))
	}
	if len(parsed.WorkPatterns) != 2 {
		t.Errorf("roundtrip work_patterns count = %d, want 2", len(parsed.WorkPatterns))
	}
}

func TestUserProfile_IsEmpty(t *testing.T) {
	p := NewUserProfile()
	if !p.IsEmpty() {
		t.Error("new profile should be empty")
	}

	p.Update("basic_info.name", "Alice")
	if p.IsEmpty() {
		t.Error("profile with data should not be empty")
	}
}

func TestUserProfile_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	original := NewUserProfile()
	original.BasicInfo["name"] = "Alice"
	original.Preferences["theme"] = "dark"
	original.Expertise = []string{"Go"}

	if err := original.Save(store); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	loaded, err := LoadProfile(store)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}

	if loaded.BasicInfo["name"] != "Alice" {
		t.Errorf("loaded name = %q, want Alice", loaded.BasicInfo["name"])
	}
	if loaded.Preferences["theme"] != "dark" {
		t.Errorf("loaded theme = %q, want dark", loaded.Preferences["theme"])
	}
}

func TestUserProfile_LoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	p, err := LoadProfile(store)
	if err != nil {
		t.Fatalf("load nonexistent: %v", err)
	}
	if !p.IsEmpty() {
		t.Error("nonexistent profile should return empty")
	}
}

func TestUserProfile_Format(t *testing.T) {
	p := NewUserProfile()
	p.BasicInfo["name"] = "Alice"

	formatted := p.Format()
	if !strings.Contains(formatted, "## Basic Info") {
		t.Error("formatted profile should contain section header")
	}
	if !strings.Contains(formatted, "**name**: Alice") {
		t.Error("formatted profile should contain name entry")
	}
}
