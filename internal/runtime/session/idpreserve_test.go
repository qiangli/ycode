package session

import (
	"testing"
)

func TestExtractIdentifiers_FilePaths(t *testing.T) {
	text := `Read the file /Users/you/projects/ycode/internal/api/provider.go and also ./cmd/ycode/main.go`
	ids := ExtractIdentifiers(text)

	paths := filterKind(ids, "file_path")
	if len(paths) < 2 {
		t.Errorf("expected at least 2 file paths, got %d: %v", len(paths), paths)
	}
}

func TestExtractIdentifiers_GitHashes(t *testing.T) {
	text := `The commit 5808e86 introduced the feature. Full hash: 5808e86abc1234567890abcdef1234567890abcd`
	ids := ExtractIdentifiers(text)

	hashes := filterKind(ids, "git_hash")
	if len(hashes) < 1 {
		t.Errorf("expected at least 1 git hash, got %d", len(hashes))
	}
}

func TestExtractIdentifiers_UUIDs(t *testing.T) {
	text := `Session ID: 550e8400-e29b-41d4-a716-446655440000`
	ids := ExtractIdentifiers(text)

	uuids := filterKind(ids, "uuid")
	if len(uuids) != 1 {
		t.Errorf("expected 1 UUID, got %d", len(uuids))
	}
	if uuids[0].Value != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("unexpected UUID: %s", uuids[0].Value)
	}
}

func TestExtractIdentifiers_GoPackages(t *testing.T) {
	text := `import "github.com/qiangli/ycode/internal/runtime/session"`
	ids := ExtractIdentifiers(text)

	pkgs := filterKind(ids, "go_package")
	if len(pkgs) < 1 {
		t.Errorf("expected at least 1 go package, got %d", len(pkgs))
	}
}

func TestExtractIdentifiers_Dedup(t *testing.T) {
	text := `/tmp/file.go and /tmp/file.go again`
	ids := ExtractIdentifiers(text)

	paths := filterKind(ids, "file_path")
	if len(paths) != 1 {
		t.Errorf("expected 1 deduped path, got %d", len(paths))
	}
}

func TestExtractIdentifiers_EmptyText(t *testing.T) {
	ids := ExtractIdentifiers("")
	if len(ids) != 0 {
		t.Errorf("expected 0 identifiers from empty text, got %d", len(ids))
	}
}

func TestFormatPreservationInstruction_Off(t *testing.T) {
	ids := []Identifier{{Value: "/tmp/file.go", Kind: "file_path"}}
	result := FormatPreservationInstruction(ids, IDPreserveOff)
	if result != "" {
		t.Error("expected empty result with IDPreserveOff")
	}
}

func TestFormatPreservationInstruction_Empty(t *testing.T) {
	result := FormatPreservationInstruction(nil, IDPreserveStrict)
	if result != "" {
		t.Error("expected empty result with no identifiers")
	}
}

func TestFormatPreservationInstruction_Strict(t *testing.T) {
	ids := []Identifier{
		{Value: "/tmp/file.go", Kind: "file_path"},
		{Value: "5808e86", Kind: "git_hash"},
	}
	result := FormatPreservationInstruction(ids, IDPreserveStrict)
	if result == "" {
		t.Error("expected non-empty preservation instruction")
	}
	if !contains(result, "/tmp/file.go") {
		t.Error("expected file path in instruction")
	}
	if !contains(result, "5808e86") {
		t.Error("expected git hash in instruction")
	}
}

func TestFormatPreservationInstruction_DeterministicOrder(t *testing.T) {
	ids := []Identifier{
		{Value: "abc", Kind: "uuid"},
		{Value: "def", Kind: "file_path"},
		{Value: "ghi", Kind: "git_hash"},
	}
	r1 := FormatPreservationInstruction(ids, IDPreserveStrict)
	r2 := FormatPreservationInstruction(ids, IDPreserveStrict)
	if r1 != r2 {
		t.Error("expected deterministic output across calls")
	}
}

func TestExtractFromMessages(t *testing.T) {
	msgs := []ConversationMessage{
		{
			Role: RoleUser,
			Content: []ContentBlock{
				{Type: ContentTypeText, Text: "Check /tmp/test.go"},
			},
		},
		{
			Role: RoleAssistant,
			Content: []ContentBlock{
				{Type: ContentTypeText, Text: "Found commit abc1234"},
			},
		},
	}
	ids := ExtractFromMessages(msgs)
	if len(ids) < 1 {
		t.Errorf("expected identifiers from messages, got %d", len(ids))
	}
}

func filterKind(ids []Identifier, kind string) []Identifier {
	var filtered []Identifier
	for _, id := range ids {
		if id.Kind == kind {
			filtered = append(filtered, id)
		}
	}
	return filtered
}
