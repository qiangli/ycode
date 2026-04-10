package scratchpad

import (
	"testing"
)

func TestManager_CRUD(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Create.
	if err := mgr.Create("test", "hello world"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Read.
	content, err := mgr.Read("test")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if content != "hello world" {
		t.Errorf("expected 'hello world', got %q", content)
	}

	// Update.
	if err := mgr.Update("test", "updated content"); err != nil {
		t.Fatalf("update: %v", err)
	}
	content, _ = mgr.Read("test")
	if content != "updated content" {
		t.Errorf("expected 'updated content', got %q", content)
	}

	// List.
	names, err := mgr.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(names) != 1 || names[0] != "test" {
		t.Errorf("expected [test], got %v", names)
	}

	// Delete.
	if err := mgr.Delete("test"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	names, _ = mgr.List()
	if len(names) != 0 {
		t.Errorf("expected empty list after delete, got %v", names)
	}
}

func TestCheckpointManager_SaveRestore(t *testing.T) {
	dir := t.TempDir()
	cm, err := NewCheckpointManager(dir)
	if err != nil {
		t.Fatalf("new checkpoint manager: %v", err)
	}

	data := map[string]string{"key": "value"}
	if err := cm.Save("cp1", "test checkpoint", data); err != nil {
		t.Fatalf("save: %v", err)
	}

	cp, err := cm.Restore("cp1")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if cp.ID != "cp1" {
		t.Errorf("expected ID 'cp1', got %q", cp.ID)
	}
	if cp.Label != "test checkpoint" {
		t.Errorf("expected label 'test checkpoint', got %q", cp.Label)
	}

	ids, err := cm.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ids) != 1 || ids[0] != "cp1" {
		t.Errorf("expected [cp1], got %v", ids)
	}
}

func TestWorkLog_AppendRead(t *testing.T) {
	dir := t.TempDir()
	wl := NewWorkLog(dir)

	if err := wl.Append("Started work on feature X"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := wl.Append("Completed feature X"); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	content, err := wl.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if content == "" {
		t.Error("work log should not be empty")
	}
}

func TestAutoCheckpointer_OnCompaction(t *testing.T) {
	dir := t.TempDir()
	cm, _ := NewCheckpointManager(dir)
	wl := NewWorkLog(dir)
	ac := NewAutoCheckpointer(cm, wl, true)

	if err := ac.OnCompaction("sess1", "summary text", 50); err != nil {
		t.Fatalf("on compaction: %v", err)
	}

	ids, _ := cm.List()
	if len(ids) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(ids))
	}
}

func TestAutoCheckpointer_Disabled(t *testing.T) {
	dir := t.TempDir()
	cm, _ := NewCheckpointManager(dir)
	ac := NewAutoCheckpointer(cm, nil, false)

	if err := ac.OnCompaction("sess1", "summary", 50); err != nil {
		t.Fatalf("should not error when disabled: %v", err)
	}

	ids, _ := cm.List()
	if len(ids) != 0 {
		t.Error("disabled checkpointer should not create checkpoints")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello_world"},
		{"Test-123", "test-123"},
		{"special!@#chars", "specialchars"},
		{"", "scratch"},
	}
	for _, tt := range tests {
		got := sanitizeName(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
