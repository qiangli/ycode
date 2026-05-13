package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestLoaderFourTierPrecedence covers the new per-project tier sitting
// between user-global and project-committed.
func TestLoaderFourTierPrecedence(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	perProjDir := filepath.Join(dir, "perproj")
	projectDir := filepath.Join(dir, "project")
	localDir := projectDir
	for _, d := range []string{userDir, perProjDir, projectDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	write := func(d, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(d, "settings.json"), []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// All four tiers set "model"; project-local (settings.local.json) wins.
	write(userDir, `{"model": "user-m", "weakModel": "user-weak"}`)
	write(perProjDir, `{"model": "perproj-m", "weakModel": "perproj-weak"}`)
	write(projectDir, `{"model": "project-m"}`)
	if err := os.WriteFile(filepath.Join(localDir, "settings.local.json"),
		[]byte(`{"model": "local-m"}`), 0o644); err != nil {
		t.Fatalf("write local: %v", err)
	}

	cfg, err := NewLoaderWithPerProject(userDir, perProjDir, projectDir, localDir).Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Model != "local-m" {
		t.Errorf("Model: got %q want local-m", cfg.Model)
	}
	// weakModel only set in user + perproj; perproj wins (closer to project tier).
	if cfg.WeakModel != "perproj-weak" {
		t.Errorf("WeakModel: got %q want perproj-weak", cfg.WeakModel)
	}
}

// TestLoaderPerProjectAppend confirms Instructions still append, not
// override, across all four tiers.
func TestLoaderPerProjectAppend(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	perProjDir := filepath.Join(dir, "perproj")
	projectDir := filepath.Join(dir, "project")
	localDir := filepath.Join(dir, "local")
	for _, d := range []string{userDir, perProjDir, projectDir, localDir} {
		_ = os.MkdirAll(d, 0o755)
	}
	_ = os.WriteFile(filepath.Join(userDir, "settings.json"),
		[]byte(`{"instructions": ["u1"]}`), 0o644)
	_ = os.WriteFile(filepath.Join(perProjDir, "settings.json"),
		[]byte(`{"instructions": ["pp1"]}`), 0o644)
	_ = os.WriteFile(filepath.Join(projectDir, "settings.json"),
		[]byte(`{"instructions": ["p1"]}`), 0o644)
	_ = os.WriteFile(filepath.Join(localDir, "settings.local.json"),
		[]byte(`{"instructions": ["loc1"]}`), 0o644)

	cfg, err := NewLoaderWithPerProject(userDir, perProjDir, projectDir, localDir).Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := cfg.Instructions
	want := []string{"u1", "pp1", "p1", "loc1"}
	if len(got) != len(want) {
		t.Fatalf("Instructions: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Instructions[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}

// TestLoaderPerProjectAbsent confirms a missing perProjectDir tier is
// silently skipped, behaving identically to the 3-tier loader.
func TestLoaderPerProjectAbsent(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	projectDir := filepath.Join(dir, "project")
	for _, d := range []string{userDir, projectDir} {
		_ = os.MkdirAll(d, 0o755)
	}
	_ = os.WriteFile(filepath.Join(userDir, "settings.json"),
		[]byte(`{"model": "user-m"}`), 0o644)
	_ = os.WriteFile(filepath.Join(projectDir, "settings.json"),
		[]byte(`{"model": "project-m"}`), 0o644)

	// Path that doesn't exist.
	cfg, err := NewLoaderWithPerProject(userDir, filepath.Join(dir, "does-not-exist"), projectDir, projectDir).Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Model != "project-m" {
		t.Errorf("Model: got %q want project-m", cfg.Model)
	}
}

// TestBootstrapLoader_ExplicitID confirms a Project.ID in the
// user-global settings is used to compute the per-project directory.
func TestBootstrapLoader_ExplicitID(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	homeAgents := filepath.Join(dir, "agents")
	cwd := filepath.Join(dir, "cwd")
	for _, d := range []string{userDir, cwd} {
		_ = os.MkdirAll(d, 0o755)
	}
	_ = os.WriteFile(filepath.Join(userDir, "settings.json"),
		[]byte(`{"project": {"id": "my-explicit"}}`), 0o644)

	loader, id := BootstrapLoader(context.Background(), userDir, homeAgents, cwd, cwd, cwd)
	if id != "my-explicit" {
		t.Fatalf("projectID: got %q want my-explicit", id)
	}
	// Per-project tier file at homeAgents/projects/my-explicit/settings.json.
	perProjDir := filepath.Join(homeAgents, "projects", "my-explicit")
	if err := os.MkdirAll(perProjDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(perProjDir, "settings.json"),
		[]byte(`{"model": "from-perproj"}`), 0o644)

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Model != "from-perproj" {
		t.Errorf("Model: got %q want from-perproj", cfg.Model)
	}
}

// TestBootstrapLoader_IgnoresPerProjectID confirms a Project.ID
// declared in the per-project file is NOT used to determine where the
// per-project file lives — otherwise the file would set its own
// location.
func TestBootstrapLoader_IgnoresPerProjectID(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	homeAgents := filepath.Join(dir, "agents")
	cwd := filepath.Join(dir, "cwd")
	for _, d := range []string{userDir, cwd} {
		_ = os.MkdirAll(d, 0o755)
	}
	// User-global has NO project.id; cwd has no git remote.
	// → Should fall back to cwd-hash.
	_, id := BootstrapLoader(context.Background(), userDir, homeAgents, cwd, cwd, cwd)
	if id == "should-be-ignored" {
		t.Fatal("project.id from non-user-global tier leaked into bootstrap")
	}
	// Plant a settings.json at projects/should-be-ignored/ declaring a
	// different id — it should not change the answer.
	wrongDir := filepath.Join(homeAgents, "projects", "should-be-ignored")
	_ = os.MkdirAll(wrongDir, 0o755)
	_ = os.WriteFile(filepath.Join(wrongDir, "settings.json"),
		[]byte(`{"project": {"id": "should-be-ignored"}}`), 0o644)

	_, id2 := BootstrapLoader(context.Background(), userDir, homeAgents, cwd, cwd, cwd)
	if id2 != id {
		t.Fatalf("BootstrapLoader is not idempotent: %q vs %q", id, id2)
	}
}
