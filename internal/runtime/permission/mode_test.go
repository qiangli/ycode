package permission

import "testing"

func TestParseMode(t *testing.T) {
	tests := []struct {
		input string
		want  Mode
	}{
		{"read-only", ReadOnly},
		{"readonly", ReadOnly},
		{"plan", ReadOnly},
		{"workspace-write", WorkspaceWrite},
		{"write", WorkspaceWrite},
		{"danger-full-access", DangerFullAccess},
		{"full", DangerFullAccess},
		{"danger", DangerFullAccess},
		{"unknown", ReadOnly}, // default fallback
	}
	for _, tt := range tests {
		got := ParseMode(tt.input)
		if got != tt.want {
			t.Errorf("ParseMode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestModeAllows(t *testing.T) {
	if !DangerFullAccess.Allows(ReadOnly) {
		t.Error("DangerFullAccess should allow ReadOnly")
	}
	if ReadOnly.Allows(WorkspaceWrite) {
		t.Error("ReadOnly should not allow WorkspaceWrite")
	}
	if !WorkspaceWrite.Allows(WorkspaceWrite) {
		t.Error("WorkspaceWrite should allow WorkspaceWrite")
	}
}
