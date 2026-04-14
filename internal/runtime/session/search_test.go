package session

import (
	"testing"
	"time"
)

func TestSearch_ByQuery(t *testing.T) {
	root := t.TempDir()

	// Create a session with a message.
	sess, err := NewWithID(root, "search-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.AddMessage(ConversationMessage{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentTypeText, Text: "hello world"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Create another session.
	sess2, err := NewWithID(root, "search-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess2.AddMessage(ConversationMessage{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentTypeText, Text: "goodbye universe"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Search for "hello".
	results, err := Search(root, SearchCriteria{Query: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "search-1" {
		t.Errorf("expected search-1, got %s", results[0].ID)
	}
}

func TestSearch_ByTitle(t *testing.T) {
	root := t.TempDir()

	// Title is not persisted in JSONL, so search falls back to GenerateDefaultTitle()
	// which uses the first user message text. Use message text that matches the filter.
	sess, err := NewWithID(root, "titled-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.AddMessage(ConversationMessage{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentTypeText, Text: "Fix the bug in parser"}},
	}); err != nil {
		t.Fatal(err)
	}

	sess2, err := NewWithID(root, "titled-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess2.AddMessage(ConversationMessage{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentTypeText, Text: "Add feature for export"}},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := Search(root, SearchCriteria{TitleLike: "bug"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "titled-1" {
		t.Errorf("expected titled-1, got %s", results[0].ID)
	}
}

func TestSearch_WithLimit(t *testing.T) {
	root := t.TempDir()

	for i := range 5 {
		id := "limit-" + string(rune('a'+i))
		s, err := NewWithID(root, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AddMessage(ConversationMessage{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentTypeText, Text: "msg"}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	results, err := Search(root, SearchCriteria{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results with limit, got %d", len(results))
	}
}

func TestSearch_EmptyDir(t *testing.T) {
	results, err := Search(t.TempDir(), SearchCriteria{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty dir, got %d", len(results))
	}
}

func TestSearch_NonexistentDir(t *testing.T) {
	results, err := Search("/nonexistent/path", SearchCriteria{})
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Errorf("expected nil for nonexistent dir")
	}
}

func TestSearch_TimeBased(t *testing.T) {
	root := t.TempDir()

	sess, err := NewWithID(root, "time-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.AddMessage(ConversationMessage{
		Role:      RoleUser,
		Content:   []ContentBlock{{Type: ContentTypeText, Text: "old message"}},
		Timestamp: time.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	// Session CreatedAt is set from first message timestamp on Load,
	// so it will be 2 hours ago. Searching "after 1 hour ago" should find nothing.
	results, err := Search(root, SearchCriteria{After: time.Now().Add(-1 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for time filter, got %d", len(results))
	}

	// Searching "after 3 hours ago" should find it.
	results, err = Search(root, SearchCriteria{After: time.Now().Add(-3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for time filter, got %d", len(results))
	}
}

func TestSearch_WithOffset(t *testing.T) {
	root := t.TempDir()

	for _, id := range []string{"off-a", "off-b", "off-c"} {
		s, err := NewWithID(root, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AddMessage(ConversationMessage{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentTypeText, Text: "msg"}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	results, err := Search(root, SearchCriteria{Offset: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result with offset+limit, got %d", len(results))
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	root := t.TempDir()

	sess, err := NewWithID(root, "case-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.AddMessage(ConversationMessage{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentTypeText, Text: "Hello World"}},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := Search(root, SearchCriteria{Query: "HELLO"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected case-insensitive match, got %d results", len(results))
	}
}
