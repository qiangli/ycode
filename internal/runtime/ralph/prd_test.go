package ralph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPRD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	prd := PRD{
		ProjectName: "test-project",
		BranchName:  "feat/test",
		Feature:     "test feature",
		Stories: []Story{
			{ID: "S1", Title: "First", Priority: 1},
			{ID: "S2", Title: "Second", Priority: 2, Passes: true},
		},
	}
	data, err := json.MarshalIndent(prd, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPRD(path)
	if err != nil {
		t.Fatalf("LoadPRD: %v", err)
	}
	if loaded.ProjectName != "test-project" {
		t.Fatalf("project = %q, want test-project", loaded.ProjectName)
	}
	if len(loaded.Stories) != 2 {
		t.Fatalf("stories = %d, want 2", len(loaded.Stories))
	}
	if loaded.Stories[1].Passes != true {
		t.Fatal("story S2 should pass")
	}
}

func TestLoadPRDNotFound(t *testing.T) {
	_, err := LoadPRD("/nonexistent/prd.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadPRDInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")
	if err := os.WriteFile(path, []byte("{bad json!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPRD(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNextStory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	prd := &PRD{
		Stories: []Story{
			{ID: "S1", Title: "Low priority", Priority: 10},
			{ID: "S2", Title: "High priority", Priority: 1},
			{ID: "S3", Title: "Mid priority", Priority: 5, Passes: true},
		},
		path: path,
	}

	next := prd.NextStory()
	if next == nil {
		t.Fatal("expected a story")
	}
	if next.ID != "S2" {
		t.Fatalf("next story = %q, want S2", next.ID)
	}
}

func TestNextStoryAllPassing(t *testing.T) {
	prd := &PRD{
		Stories: []Story{
			{ID: "S1", Passes: true},
			{ID: "S2", Passes: true},
		},
	}
	if prd.NextStory() != nil {
		t.Fatal("expected nil when all pass")
	}
}

func TestUpdateStory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prd.json")

	prd := &PRD{
		Stories: []Story{
			{ID: "S1", Title: "First"},
			{ID: "S2", Title: "Second"},
		},
		path: path,
	}
	// Save initial so UpdateStory can persist.
	if err := prd.Save(); err != nil {
		t.Fatal(err)
	}

	if err := prd.UpdateStory("S1", true, "done"); err != nil {
		t.Fatalf("UpdateStory: %v", err)
	}
	if !prd.Stories[0].Passes {
		t.Fatal("S1 should pass")
	}
	if len(prd.Stories[0].Notes) != 1 || prd.Stories[0].Notes[0] != "done" {
		t.Fatalf("notes = %v", prd.Stories[0].Notes)
	}

	// Verify persisted.
	loaded, err := LoadPRD(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Stories[0].Passes {
		t.Fatal("persisted S1 should pass")
	}
}

func TestUpdateStoryNotFound(t *testing.T) {
	dir := t.TempDir()
	prd := &PRD{
		Stories: []Story{{ID: "S1"}},
		path:    filepath.Join(dir, "prd.json"),
	}
	if err := prd.UpdateStory("NOPE", true, ""); err == nil {
		t.Fatal("expected error for missing story")
	}
}

func TestAllPass(t *testing.T) {
	prd := &PRD{
		Stories: []Story{
			{ID: "S1", Passes: true},
			{ID: "S2", Passes: false},
		},
	}
	if prd.AllPass() {
		t.Fatal("should not all pass")
	}
	prd.Stories[1].Passes = true
	if !prd.AllPass() {
		t.Fatal("should all pass")
	}
}

func TestAllPassEmpty(t *testing.T) {
	prd := &PRD{}
	if !prd.AllPass() {
		t.Fatal("empty stories should return true")
	}
}

func TestProgress(t *testing.T) {
	prd := &PRD{
		Stories: []Story{
			{ID: "S1", Passes: true},
			{ID: "S2", Passes: false},
			{ID: "S3", Passes: true},
		},
	}
	done, total := prd.Progress()
	if done != 2 || total != 3 {
		t.Fatalf("progress = %d/%d, want 2/3", done, total)
	}
}

func TestSaveNoPath(t *testing.T) {
	prd := &PRD{}
	if err := prd.Save(); err == nil {
		t.Fatal("expected error when no path set")
	}
}
