package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewChildSession(t *testing.T) {
	parentDir := t.TempDir()

	child, err := NewChildSession(parentDir, "parent-123", "Explore", 1)
	if err != nil {
		t.Fatalf("NewChildSession: %v", err)
	}

	if child.ParentID != "parent-123" {
		t.Errorf("expected parent-123, got %s", child.ParentID)
	}
	if child.AgentType != "Explore" {
		t.Errorf("expected Explore, got %s", child.AgentType)
	}
	if child.Depth != 1 {
		t.Errorf("expected depth 1, got %d", child.Depth)
	}

	// Check metadata file exists.
	metaPath := filepath.Join(child.Dir, "metadata.json")
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("metadata file should exist: %v", err)
	}

	// Add a message to verify isolation.
	err = child.AddMessage(ConversationMessage{
		Role: RoleUser,
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: "hello from child"},
		},
	})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	// Verify messages.jsonl in child dir.
	msgPath := filepath.Join(child.Dir, "messages.jsonl")
	if _, err := os.Stat(msgPath); err != nil {
		t.Errorf("messages.jsonl should exist in child dir: %v", err)
	}
}

func TestListChildSessions(t *testing.T) {
	parentDir := t.TempDir()

	// No children yet.
	children, err := ListChildSessions(parentDir)
	if err != nil {
		t.Fatalf("ListChildSessions: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("expected 0 children, got %d", len(children))
	}

	// Create two children.
	_, err = NewChildSession(parentDir, "parent-1", "Explore", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewChildSession(parentDir, "parent-1", "Plan", 1)
	if err != nil {
		t.Fatal(err)
	}

	children, err = ListChildSessions(parentDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Errorf("expected 2 children, got %d", len(children))
	}
}

func TestChildSession_MessageIsolation(t *testing.T) {
	parentDir := t.TempDir()

	// Create parent session dir structure.
	parentID := "parent-456"
	parentMsgDir := parentDir
	os.MkdirAll(parentMsgDir, 0o755)

	// Create parent session.
	parent := &Session{
		ID:  parentID,
		Dir: parentMsgDir,
	}
	_ = parent.AddMessage(ConversationMessage{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentTypeText, Text: "parent message"}},
	})

	// Create child.
	child, err := NewChildSession(parentDir, parentID, "Explore", 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = child.AddMessage(ConversationMessage{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentTypeText, Text: "child message"}},
	})

	// Parent should have 1 message, child should have 1 message.
	if len(parent.Messages) != 1 {
		t.Errorf("parent: expected 1 message, got %d", len(parent.Messages))
	}
	if len(child.Messages) != 1 {
		t.Errorf("child: expected 1 message, got %d", len(child.Messages))
	}
}
