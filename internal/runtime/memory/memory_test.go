package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Manager: lifecycle tests
// ---------------------------------------------------------------------------

func TestMemoryManager_SaveRecallForget(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	mem := &Memory{
		Name:        "test-memory",
		Description: "a test memory for unit testing",
		Type:        TypeUser,
		Content:     "User prefers Go over Rust",
		FilePath:    filepath.Join(dir, "test-memory.md"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := mgr.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}

	results, err := mgr.Recall("test", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one recall result")
	}

	all, err := mgr.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(all))
	}

	if err := mgr.Forget("test-memory"); err != nil {
		t.Fatalf("forget: %v", err)
	}

	all, _ = mgr.All()
	if len(all) != 0 {
		t.Errorf("expected 0 memories after forget, got %d", len(all))
	}
}

func TestMemoryManager_FullLifecycle(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Save initial memory.
	mem := &Memory{
		Name:        "lifecycle-mem",
		Description: "initial description",
		Type:        TypeProject,
		Content:     "Initial content",
	}
	if err := mgr.Save(mem); err != nil {
		t.Fatalf("save initial: %v", err)
	}

	// Verify recall finds it.
	results, _ := mgr.Recall("lifecycle", 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Update (re-save with same filepath).
	mem.Description = "updated description"
	mem.Content = "Updated content"
	if err := mgr.Save(mem); err != nil {
		t.Fatalf("save update: %v", err)
	}

	// Verify updated content persists.
	all, _ := mgr.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 memory after update, got %d", len(all))
	}
	if all[0].Content != "Updated content" {
		t.Errorf("expected updated content, got %q", all[0].Content)
	}

	// Verify index updated.
	idx, _ := mgr.ReadIndex()
	if !strings.Contains(idx, "updated description") {
		t.Errorf("index should contain updated description, got:\n%s", idx)
	}

	// Forget.
	if err := mgr.Forget("lifecycle-mem"); err != nil {
		t.Fatalf("forget: %v", err)
	}
	all, _ = mgr.All()
	if len(all) != 0 {
		t.Errorf("expected 0 after forget, got %d", len(all))
	}

	// Verify index cleaned up.
	idx, _ = mgr.ReadIndex()
	if strings.Contains(idx, "lifecycle-mem") {
		t.Errorf("index should not contain forgotten memory")
	}
}

func TestMemoryManager_ForgetNonexistent(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	err = mgr.Forget("does-not-exist")
	if err == nil {
		t.Error("expected error when forgetting nonexistent memory")
	}
}

func TestMemoryManager_SaveMultipleThenForgetMiddle(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	for i := 0; i < 5; i++ {
		mem := &Memory{
			Name:        fmt.Sprintf("mem-%d", i),
			Description: fmt.Sprintf("memory number %d", i),
			Type:        TypeUser,
			Content:     fmt.Sprintf("Content %d", i),
		}
		if err := mgr.Save(mem); err != nil {
			t.Fatalf("save mem-%d: %v", i, err)
		}
	}

	all, _ := mgr.All()
	if len(all) != 5 {
		t.Fatalf("expected 5 memories, got %d", len(all))
	}

	// Forget middle one.
	if err := mgr.Forget("mem-2"); err != nil {
		t.Fatalf("forget mem-2: %v", err)
	}

	all, _ = mgr.All()
	if len(all) != 4 {
		t.Fatalf("expected 4 memories, got %d", len(all))
	}

	// Verify the right one is gone.
	for _, m := range all {
		if m.Name == "mem-2" {
			t.Error("mem-2 should have been forgotten")
		}
	}

	// Verify index doesn't contain forgotten entry.
	idx, _ := mgr.ReadIndex()
	if strings.Contains(idx, "mem-2.md") {
		t.Error("index should not contain mem-2 after forget")
	}
}

// ---------------------------------------------------------------------------
// Manager: index tests
// ---------------------------------------------------------------------------

func TestMemoryManager_Index(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	mem := &Memory{
		Name:        "indexed-mem",
		Description: "memory with index entry",
		Type:        TypeProject,
		Content:     "Some project info",
		FilePath:    filepath.Join(dir, "indexed-mem.md"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := mgr.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}

	indexContent, err := mgr.ReadIndex()
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if indexContent == "" {
		t.Error("index should not be empty after save")
	}
}

// ---------------------------------------------------------------------------
// Store: golden file / roundtrip tests
// ---------------------------------------------------------------------------

func TestStore_LoadGoldenFiles(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	tests := []struct {
		file        string
		wantName    string
		wantType    Type
		wantDesc    string
		wantContent string // substring check
	}{
		{
			file:        "testdata/user_preferences.md",
			wantName:    "user-preferences",
			wantType:    TypeUser,
			wantDesc:    "User coding preferences and environment setup",
			wantContent: "Go over Rust",
		},
		{
			file:        "testdata/feedback_testing.md",
			wantName:    "feedback-testing",
			wantType:    TypeFeedback,
			wantDesc:    "Don't mock the database in integration tests",
			wantContent: "real database",
		},
		{
			file:        "testdata/project_deadline.md",
			wantName:    "project-deadline",
			wantType:    TypeProject,
			wantDesc:    "Merge freeze for mobile release on 2026-03-05",
			wantContent: "2026-03-05",
		},
		{
			file:        "testdata/reference_dashboard.md",
			wantName:    "reference-dashboard",
			wantType:    TypeReference,
			wantDesc:    "Grafana oncall latency dashboard for API monitoring",
			wantContent: "api-latency",
		},
		{
			file:        "testdata/unicode_content.md",
			wantName:    "unicode-test",
			wantType:    TypeUser,
			wantDesc:    "Memory with unicode characters and CJK text",
			wantContent: "用户偏好中文文档",
		},
		{
			file:        "testdata/multiline_content.md",
			wantName:    "multiline-complex",
			wantType:    TypeReference,
			wantDesc:    "Memory with complex multi-line content including code blocks",
			wantContent: "func main()",
		},
	}

	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			mem, err := store.Load(tc.file)
			if err != nil {
				t.Fatalf("load %s: %v", tc.file, err)
			}
			if mem.Name != tc.wantName {
				t.Errorf("name: got %q, want %q", mem.Name, tc.wantName)
			}
			if mem.Type != tc.wantType {
				t.Errorf("type: got %q, want %q", mem.Type, tc.wantType)
			}
			if mem.Description != tc.wantDesc {
				t.Errorf("desc: got %q, want %q", mem.Description, tc.wantDesc)
			}
			if !strings.Contains(mem.Content, tc.wantContent) {
				t.Errorf("content should contain %q, got:\n%s", tc.wantContent, mem.Content)
			}
		})
	}
}

func TestStore_LoadNoFrontmatter(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	mem, err := store.Load("testdata/no_frontmatter.md")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if mem.Name != "" {
		t.Errorf("expected empty name, got %q", mem.Name)
	}
	if mem.Type != "" {
		t.Errorf("expected empty type, got %q", mem.Type)
	}
	if !strings.Contains(mem.Content, "no frontmatter") {
		t.Errorf("content should contain full text, got: %s", mem.Content)
	}
}

func TestStore_SaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	original := &Memory{
		Name:        "roundtrip",
		Description: "test roundtrip persistence",
		Type:        TypeFeedback,
		Content:     "This is test content\nwith multiple lines\nand special chars: <>&\"'",
		FilePath:    filepath.Join(dir, "roundtrip.md"),
	}

	if err := store.Save(original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load(original.FilePath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Name != original.Name {
		t.Errorf("name: got %q, want %q", loaded.Name, original.Name)
	}
	if loaded.Type != original.Type {
		t.Errorf("type: got %q, want %q", loaded.Type, original.Type)
	}
	if loaded.Description != original.Description {
		t.Errorf("desc: got %q, want %q", loaded.Description, original.Description)
	}
	if loaded.Content != original.Content {
		t.Errorf("content: got %q, want %q", loaded.Content, original.Content)
	}
}

func TestMemoryFileRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	mem := &Memory{
		Name:        "roundtrip",
		Description: "test roundtrip",
		Type:        TypeFeedback,
		Content:     "This is test content\nwith multiple lines",
		FilePath:    filepath.Join(dir, "roundtrip.md"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := store.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := os.Stat(mem.FilePath); err != nil {
		t.Fatalf("file should exist: %v", err)
	}

	memories, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0].Name != "roundtrip" {
		t.Errorf("expected name 'roundtrip', got %q", memories[0].Name)
	}
}

// ---------------------------------------------------------------------------
// Store: error handling tests
// ---------------------------------------------------------------------------

func TestStore_LoadNonexistent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, err = store.Load("/nonexistent/path/file.md")
	if err == nil {
		t.Error("expected error loading nonexistent file")
	}
}

func TestStore_MalformedFrontmatter(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	// Incomplete frontmatter (no closing ---).
	path := filepath.Join(dir, "malformed.md")
	os.WriteFile(path, []byte("---\nname: broken\nno closing delimiter"), 0o644)

	mem, err := store.Load(path)
	if err != nil {
		t.Fatalf("load should not error on malformed frontmatter: %v", err)
	}
	// The entire content should be treated as content (no frontmatter parsed).
	if mem.Name != "" {
		t.Errorf("expected empty name for malformed frontmatter, got %q", mem.Name)
	}
}

func TestStore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	path := filepath.Join(dir, "empty.md")
	os.WriteFile(path, []byte(""), 0o644)

	mem, err := store.Load(path)
	if err != nil {
		t.Fatalf("load empty file: %v", err)
	}
	if mem.Name != "" || mem.Content != "" {
		t.Errorf("expected empty memory, got name=%q content=%q", mem.Name, mem.Content)
	}
}

func TestStore_BinaryContent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	path := filepath.Join(dir, "binary.md")
	os.WriteFile(path, []byte{0x00, 0xFF, 0xFE, 0x89, 0x50, 0x4E, 0x47}, 0o644)

	// Should not panic.
	mem, err := store.Load(path)
	if err != nil {
		t.Fatalf("load binary: %v", err)
	}
	// Just verify it doesn't crash; content will be garbage but non-nil.
	_ = mem
}

func TestStore_ListEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	memories, err := store.List()
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories in empty store, got %d", len(memories))
	}
}

func TestStore_ListExcludesMEMORYmd(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	// Create MEMORY.md (index file) and a real memory file.
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("- [test](test.md)"), 0o644)
	os.WriteFile(filepath.Join(dir, "test.md"), []byte("---\nname: test\ntype: user\n---\ncontent"), 0o644)

	memories, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory (excluding MEMORY.md), got %d", len(memories))
	}
	if memories[0].Name != "test" {
		t.Errorf("expected name 'test', got %q", memories[0].Name)
	}
}

func TestStore_SaveAutoFilePath(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	mem := &Memory{
		Name:    "Auto Path Test",
		Type:    TypeUser,
		Content: "content",
	}
	if err := store.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}

	// FilePath should be auto-generated.
	if mem.FilePath == "" {
		t.Fatal("expected auto-generated file path")
	}
	if !strings.HasSuffix(mem.FilePath, ".md") {
		t.Errorf("expected .md extension, got %s", mem.FilePath)
	}

	// Verify the file exists.
	if _, err := os.Stat(mem.FilePath); err != nil {
		t.Fatalf("auto-generated file should exist: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Store: sanitizeFilename
// ---------------------------------------------------------------------------

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"With Spaces", "with_spaces"},
		{"UPPER-case", "upper-case"},
		{"special!@#$%chars", "specialchars"},
		{"unicode-日本語", "unicode-"},
		{"", "memory"},
		{"a very long name that exceeds the fifty character maximum limit for filenames", "a_very_long_name_that_exceeds_the_fifty_character_"},
		{"---dashes---", "---dashes---"},
		{"under_score", "under_score"},
		{"MiXeD CaSe With 123 Numbers", "mixed_case_with_123_numbers"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeFilename(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Store: parseFrontmatter edge cases
// ---------------------------------------------------------------------------

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantName    string
		wantType    Type
		wantContent string
	}{
		{
			name:        "valid frontmatter",
			input:       "---\nname: test\ndescription: desc\ntype: user\n---\n\ncontent here",
			wantName:    "test",
			wantType:    TypeUser,
			wantContent: "content here",
		},
		{
			name:        "no frontmatter",
			input:       "just plain content",
			wantName:    "",
			wantType:    "",
			wantContent: "just plain content",
		},
		{
			name:        "incomplete frontmatter",
			input:       "---\nname: broken",
			wantName:    "",
			wantType:    "",
			wantContent: "---\nname: broken",
		},
		{
			name:        "empty frontmatter",
			input:       "---\n\n---\n\ncontent only",
			wantName:    "",
			wantType:    "",
			wantContent: "content only",
		},
		{
			name:        "frontmatter with extra fields",
			input:       "---\nname: test\nunknown: ignored\ntype: feedback\n---\n\nbody",
			wantName:    "test",
			wantType:    TypeFeedback,
			wantContent: "body",
		},
		{
			name:        "frontmatter with colons in value",
			input:       "---\nname: test\ndescription: value: with: colons\ntype: project\n---\n\nok",
			wantName:    "test",
			wantType:    TypeProject,
			wantContent: "ok",
		},
		{
			name:        "empty string",
			input:       "",
			wantName:    "",
			wantType:    "",
			wantContent: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mem := parseFrontmatter(tc.input)
			if mem.Name != tc.wantName {
				t.Errorf("name: got %q, want %q", mem.Name, tc.wantName)
			}
			if mem.Type != tc.wantType {
				t.Errorf("type: got %q, want %q", mem.Type, tc.wantType)
			}
			if mem.Content != tc.wantContent {
				t.Errorf("content: got %q, want %q", mem.Content, tc.wantContent)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Index tests
// ---------------------------------------------------------------------------

func TestIndex_MaxLines(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(dir)

	// Add more entries than MaxIndexLines.
	for i := 0; i < MaxIndexLines+50; i++ {
		err := idx.AddEntry(
			fmt.Sprintf("mem-%d", i),
			fmt.Sprintf("mem-%d.md", i),
			fmt.Sprintf("description %d", i),
		)
		if err != nil {
			t.Fatalf("add entry %d: %v", i, err)
		}
	}

	content, err := idx.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	lines := strings.Split(content, "\n")
	if len(lines) > MaxIndexLines+1 { // +1 for potential trailing newline
		t.Errorf("index has %d lines, should be <= %d", len(lines), MaxIndexLines)
	}
}

func TestIndex_UpdateExistingEntry(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(dir)

	idx.AddEntry("mem-1", "mem-1.md", "original description")
	idx.AddEntry("mem-1", "mem-1.md", "updated description")

	content, _ := idx.Read()
	if strings.Count(content, "mem-1.md") != 1 {
		t.Errorf("expected exactly one entry for mem-1.md, got:\n%s", content)
	}
	if !strings.Contains(content, "updated description") {
		t.Error("expected updated description in index")
	}
}

func TestIndex_RemoveEntry(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(dir)

	idx.AddEntry("mem-1", "mem-1.md", "desc 1")
	idx.AddEntry("mem-2", "mem-2.md", "desc 2")
	idx.AddEntry("mem-3", "mem-3.md", "desc 3")

	idx.RemoveEntry("mem-2.md")

	content, _ := idx.Read()
	if strings.Contains(content, "mem-2.md") {
		t.Error("mem-2.md should be removed from index")
	}
	if !strings.Contains(content, "mem-1.md") || !strings.Contains(content, "mem-3.md") {
		t.Error("other entries should remain in index")
	}
}

func TestIndex_RemoveFromEmpty(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(dir)

	// Should not error on nonexistent index file.
	err := idx.RemoveEntry("nonexistent.md")
	if err != nil {
		t.Errorf("remove from empty index should not error: %v", err)
	}
}

func TestIndex_ReadEmpty(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(dir)

	content, err := idx.Read()
	if err != nil {
		t.Fatalf("read empty: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

// ---------------------------------------------------------------------------
// Staleness: table-driven, all types and boundaries
// ---------------------------------------------------------------------------

func TestStaleness(t *testing.T) {
	recent := &Memory{
		Type:      TypeProject,
		UpdatedAt: time.Now(),
	}
	if IsStale(recent) {
		t.Error("recent memory should not be stale")
	}

	old := &Memory{
		Type:      TypeProject,
		UpdatedAt: time.Now().Add(-60 * 24 * time.Hour),
	}
	if !IsStale(old) {
		t.Error("old memory should be stale")
	}
}

func TestStaleness_AllTypes(t *testing.T) {
	day := 24 * time.Hour

	tests := []struct {
		name      string
		typ       Type
		ageDays   int
		wantStale bool
	}{
		// Project: 30 days
		{"project fresh", TypeProject, 1, false},
		{"project boundary-under", TypeProject, 29, false},
		{"project boundary-over", TypeProject, 31, true},
		{"project old", TypeProject, 90, true},

		// Reference: 90 days
		{"reference fresh", TypeReference, 1, false},
		{"reference boundary-under", TypeReference, 89, false},
		{"reference boundary-over", TypeReference, 91, true},

		// User: 180 days
		{"user fresh", TypeUser, 1, false},
		{"user boundary-under", TypeUser, 179, false},
		{"user boundary-over", TypeUser, 181, true},

		// Feedback: 365 days
		{"feedback fresh", TypeFeedback, 1, false},
		{"feedback boundary-under", TypeFeedback, 364, false},
		{"feedback boundary-over", TypeFeedback, 366, true},

		// Unknown type: default 90 days
		{"unknown fresh", Type("custom"), 1, false},
		{"unknown boundary-over", Type("custom"), 91, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mem := &Memory{
				Type:      tc.typ,
				UpdatedAt: time.Now().Add(-time.Duration(tc.ageDays) * day),
			}
			got := IsStale(mem)
			if got != tc.wantStale {
				t.Errorf("IsStale(type=%s, age=%dd) = %v, want %v", tc.typ, tc.ageDays, got, tc.wantStale)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DecayScore tests
// ---------------------------------------------------------------------------

func TestDecayScore(t *testing.T) {
	fresh := &Memory{UpdatedAt: time.Now()}
	score := DecayScore(1.0, fresh)
	if score != 1.0 {
		t.Errorf("fresh memory should have no decay, got %f", score)
	}

	old := &Memory{UpdatedAt: time.Now().Add(-90 * 24 * time.Hour)}
	decayed := DecayScore(1.0, old)
	if decayed >= 1.0 {
		t.Errorf("old memory should have decayed score, got %f", decayed)
	}
}

func TestDecayScore_Table(t *testing.T) {
	day := 24 * time.Hour

	tests := []struct {
		name     string
		ageDays  int
		wantMin  float64 // inclusive
		wantMax  float64 // inclusive
		baseScore float64
	}{
		{"within grace period (0 days)", 0, 1.0, 1.0, 1.0},
		{"within grace period (3 days)", 3, 1.0, 1.0, 1.0},
		{"within grace period (6 days)", 6, 1.0, 1.0, 1.0},
		{"just after grace (8 days)", 8, 0.5, 1.0, 1.0},
		{"30 days", 30, 0.1, 0.9, 1.0},
		{"90 days", 90, 0.05, 0.5, 1.0},
		{"365 days", 365, 0.01, 0.2, 1.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mem := &Memory{UpdatedAt: time.Now().Add(-time.Duration(tc.ageDays) * day)}
			got := DecayScore(tc.baseScore, mem)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("DecayScore(age=%dd) = %f, want [%f, %f]", tc.ageDays, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestDecayScore_Monotonic(t *testing.T) {
	day := 24 * time.Hour
	prev := DecayScore(1.0, &Memory{UpdatedAt: time.Now()})

	for _, days := range []int{7, 14, 30, 60, 90, 180, 365} {
		mem := &Memory{UpdatedAt: time.Now().Add(-time.Duration(days) * day)}
		score := DecayScore(1.0, mem)
		if score > prev {
			t.Errorf("decay should be monotonically non-increasing: day %d (%f) > previous (%f)", days, score, prev)
		}
		prev = score
	}
}

func TestDecayScore_ScalesWithBase(t *testing.T) {
	mem := &Memory{UpdatedAt: time.Now().Add(-60 * 24 * time.Hour)}
	s1 := DecayScore(1.0, mem)
	s2 := DecayScore(2.0, mem)
	s3 := DecayScore(0.5, mem)

	if s2 < s1 {
		t.Error("higher base score should produce higher decayed score")
	}
	if s3 > s1 {
		t.Error("lower base score should produce lower decayed score")
	}
}

// ---------------------------------------------------------------------------
// Search tests
// ---------------------------------------------------------------------------

func TestSearch(t *testing.T) {
	memories := []*Memory{
		{Name: "api-config", Description: "API configuration details", Type: TypeReference},
		{Name: "user-pref", Description: "user prefers dark mode", Type: TypeUser},
		{Name: "project-goal", Description: "project goal is to build a CLI tool", Type: TypeProject},
	}

	results := Search(memories, "API", 5)
	if len(results) == 0 {
		t.Fatal("expected at least one search result for 'API'")
	}
	if results[0].Memory.Name != "api-config" {
		t.Errorf("expected api-config first, got %s", results[0].Memory.Name)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	memories := []*Memory{
		{Name: "test", Description: "test", Content: "test"},
	}
	results := Search(memories, "", 5)
	if len(results) != 0 {
		t.Errorf("empty query should return no results, got %d", len(results))
	}
}

func TestSearch_EmptyMemories(t *testing.T) {
	results := Search(nil, "test", 5)
	if len(results) != 0 {
		t.Errorf("nil memories should return no results, got %d", len(results))
	}

	results = Search([]*Memory{}, "test", 5)
	if len(results) != 0 {
		t.Errorf("empty memories should return no results, got %d", len(results))
	}
}

func TestSearch_NoMatches(t *testing.T) {
	memories := []*Memory{
		{Name: "alpha", Description: "first memory", Content: "content one"},
	}
	results := Search(memories, "zzzznonexistent", 5)
	if len(results) != 0 {
		t.Errorf("expected no matches, got %d", len(results))
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	memories := []*Memory{
		{Name: "API-Config", Description: "API Configuration", Content: "REST API details"},
	}

	for _, query := range []string{"api", "API", "Api", "aPi"} {
		results := Search(memories, query, 5)
		if len(results) == 0 {
			t.Errorf("query %q should match case-insensitively", query)
		}
	}
}

func TestSearch_ScoringWeights(t *testing.T) {
	memories := []*Memory{
		{Name: "deploy", Description: "unrelated description", Content: "unrelated content"},
		{Name: "unrelated", Description: "deploy info", Content: "unrelated content"},
		{Name: "unrelated", Description: "unrelated", Content: "deploy instructions"},
	}

	results := Search(memories, "deploy", 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Name match (3.0) > Description match (2.0) > Content match (1.0).
	if results[0].Memory.Name != "deploy" {
		t.Errorf("name match should rank first, got %q", results[0].Memory.Name)
	}
	if results[0].Score <= results[1].Score {
		t.Error("name match should score higher than description match")
	}
	if results[1].Score <= results[2].Score {
		t.Error("description match should score higher than content match")
	}
}

func TestSearch_MaxResults(t *testing.T) {
	var memories []*Memory
	for i := 0; i < 20; i++ {
		memories = append(memories, &Memory{
			Name:    fmt.Sprintf("test-%d", i),
			Content: "test content",
		})
	}

	results := Search(memories, "test", 5)
	if len(results) != 5 {
		t.Errorf("expected 5 results (maxResults=5), got %d", len(results))
	}
}

func TestSearch_MultiWordQuery(t *testing.T) {
	memories := []*Memory{
		{Name: "api-auth", Description: "API authentication setup", Content: "OAuth2 flow"},
		{Name: "api-config", Description: "API config", Content: "just config"},
		{Name: "auth-flow", Description: "authentication flow", Content: "login steps"},
	}

	results := Search(memories, "API authentication", 5)
	if len(results) == 0 {
		t.Fatal("expected results for multi-word query")
	}
	// api-auth should score highest (matches both "api" in name + "authentication" in description).
	if results[0].Memory.Name != "api-auth" {
		t.Errorf("expected api-auth first, got %q", results[0].Memory.Name)
	}
}

// ---------------------------------------------------------------------------
// Discovery tests
// ---------------------------------------------------------------------------

func TestDiscoverMemoryFiles(t *testing.T) {
	// Create a directory tree with memory files at various levels.
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b", "c")
	os.MkdirAll(sub, 0o755)

	// Place files at different levels.
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("root claude"), 0o644)
	os.WriteFile(filepath.Join(root, "a", "MEMORY.md"), []byte("mid memory"), 0o644)
	os.WriteFile(filepath.Join(sub, "CLAUDE.md"), []byte("leaf claude"), 0o644)
	os.WriteFile(filepath.Join(sub, "CLAUDE.local.md"), []byte("leaf local"), 0o644)

	// Discover from the deepest directory.
	paths := DiscoverMemoryFiles(sub)

	if len(paths) == 0 {
		t.Fatal("expected discovered files")
	}

	// Should find files from sub up to root.
	var foundRootClaude, foundMidMemory, foundLeafClaude, foundLeafLocal bool
	for _, p := range paths {
		switch {
		case p == filepath.Join(root, "CLAUDE.md"):
			foundRootClaude = true
		case p == filepath.Join(root, "a", "MEMORY.md"):
			foundMidMemory = true
		case p == filepath.Join(sub, "CLAUDE.md"):
			foundLeafClaude = true
		case p == filepath.Join(sub, "CLAUDE.local.md"):
			foundLeafLocal = true
		}
	}

	if !foundRootClaude {
		t.Error("should discover root CLAUDE.md")
	}
	if !foundMidMemory {
		t.Error("should discover mid-level MEMORY.md")
	}
	if !foundLeafClaude {
		t.Error("should discover leaf CLAUDE.md")
	}
	if !foundLeafLocal {
		t.Error("should discover leaf CLAUDE.local.md")
	}
}

func TestDiscoverMemoryFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	paths := DiscoverMemoryFiles(dir)
	// May find files above tmpdir, but should not panic.
	_ = paths
}

// ---------------------------------------------------------------------------
// Dreamer tests
// ---------------------------------------------------------------------------

func TestDreamer_DisabledNoOp(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)
	d := NewDreamer(mgr, false)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start should return nil immediately when disabled.
	err := d.Start(ctx)
	if err != nil {
		t.Errorf("disabled dreamer should return nil, got: %v", err)
	}
}

func TestDreamer_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)
	d := NewDreamer(mgr, true)
	// Use a very short interval for testing.
	d.interval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- d.Start(ctx)
	}()

	// Let it run briefly then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dreamer did not stop after context cancellation")
	}
}

func TestDreamer_RemovesStaleMemories(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	// Save a stale project memory (>30 days old).
	staleMem := &Memory{
		Name:        "stale-project",
		Description: "old project info",
		Type:        TypeProject,
		Content:     "outdated",
	}
	mgr.Save(staleMem)

	// Manually set the file modification time to 60 days ago.
	os.Chtimes(staleMem.FilePath, time.Now().Add(-60*24*time.Hour), time.Now().Add(-60*24*time.Hour))

	// Save a fresh memory.
	freshMem := &Memory{
		Name:        "fresh-user",
		Description: "fresh user info",
		Type:        TypeUser,
		Content:     "current",
	}
	mgr.Save(freshMem)

	// Run consolidation directly.
	d := NewDreamer(mgr, true)
	if err := d.consolidate(); err != nil {
		t.Fatalf("consolidate: %v", err)
	}

	all, _ := mgr.All()
	for _, m := range all {
		if m.Name == "stale-project" {
			t.Error("stale memory should have been removed")
		}
	}
	// Fresh memory should survive.
	found := false
	for _, m := range all {
		if m.Name == "fresh-user" {
			found = true
		}
	}
	if !found {
		t.Error("fresh memory should survive consolidation")
	}
}

func TestDreamer_MergesSimilarProjects(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	// Save two project memories with similar descriptions.
	mem1 := &Memory{
		Name:        "proj-info-1",
		Description: "project deployment configuration",
		Type:        TypeProject,
		Content:     "Old deployment info",
	}
	mgr.Save(mem1)

	mem2 := &Memory{
		Name:        "proj-info-2",
		Description: "project deployment configuration",
		Type:        TypeProject,
		Content:     "New deployment info",
	}
	mgr.Save(mem2)

	// Save a non-similar memory.
	mem3 := &Memory{
		Name:        "proj-other",
		Description: "completely different topic",
		Type:        TypeProject,
		Content:     "Other info",
	}
	mgr.Save(mem3)

	d := NewDreamer(mgr, true)
	if err := d.consolidate(); err != nil {
		t.Fatalf("consolidate: %v", err)
	}

	all, _ := mgr.All()
	// Should have merged the similar ones (keeping 1) + the different one = 2.
	projCount := 0
	for _, m := range all {
		if m.Type == TypeProject {
			projCount++
		}
	}
	if projCount != 2 {
		t.Errorf("expected 2 project memories after merge, got %d", projCount)
	}
}

func TestDreamer_ConsolidateEmpty(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)
	d := NewDreamer(mgr, true)

	// Should not error on empty store.
	if err := d.consolidate(); err != nil {
		t.Errorf("consolidate empty: %v", err)
	}
}

func TestDreamer_EnableDisable(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)
	d := NewDreamer(mgr, false)

	if d.IsEnabled() {
		t.Error("should start disabled")
	}

	d.SetEnabled(true)
	if !d.IsEnabled() {
		t.Error("should be enabled after SetEnabled(true)")
	}

	d.SetEnabled(false)
	if d.IsEnabled() {
		t.Error("should be disabled after SetEnabled(false)")
	}
}

func TestNormalizeDescription(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Simple Description", "simple description"},
		{"  extra   spaces  ", "extra spaces"},
		{"short", "short"},
		{strings.Repeat("a", 100), strings.Repeat("a", 60)},
		{"", ""},
		{"MiXeD CaSe", "mixed case"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeDescription(tc.input)
			if got != tc.want {
				t.Errorf("normalizeDescription(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrency tests
// ---------------------------------------------------------------------------

func TestManager_ConcurrentSave(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mem := &Memory{
				Name:        fmt.Sprintf("concurrent-%d", i),
				Description: fmt.Sprintf("concurrent memory %d", i),
				Type:        TypeUser,
				Content:     fmt.Sprintf("content %d", i),
			}
			if err := mgr.Save(mem); err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent save error: %v", err)
	}

	all, err := mgr.All()
	if err != nil {
		t.Fatalf("list after concurrent saves: %v", err)
	}
	if len(all) != n {
		t.Errorf("expected %d memories, got %d", n, len(all))
	}
}

func TestManager_ConcurrentRecall(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Pre-populate.
	for i := 0; i < 10; i++ {
		mgr.Save(&Memory{
			Name:        fmt.Sprintf("recall-%d", i),
			Description: fmt.Sprintf("recall memory %d", i),
			Type:        TypeUser,
			Content:     "searchable content",
		})
	}

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := mgr.Recall("recall", 5)
			if err != nil {
				t.Errorf("concurrent recall: %v", err)
			}
			if len(results) == 0 {
				t.Error("expected results from concurrent recall")
			}
		}()
	}
	wg.Wait()
}

func TestManager_ConcurrentSaveAndRecall(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// Writers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
			}
			mgr.Save(&Memory{
				Name:        fmt.Sprintf("mixed-%d", i),
				Description: fmt.Sprintf("mixed %d", i),
				Type:        TypeProject,
				Content:     "mixed content",
			})
		}(i)
	}

	// Readers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
			}
			mgr.Recall("mixed", 5)
		}()
	}

	wg.Wait()

	// Just verify we didn't panic or deadlock.
	all, _ := mgr.All()
	if len(all) == 0 {
		t.Error("expected some memories after concurrent save+recall")
	}
}
