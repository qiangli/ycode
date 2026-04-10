package permission

// Mode defines the permission level for tool execution.
type Mode int

const (
	// ReadOnly allows only read operations (file reads, searches).
	ReadOnly Mode = iota
	// WorkspaceWrite allows reads and writes within the workspace.
	WorkspaceWrite
	// DangerFullAccess allows all operations including bash, external APIs.
	DangerFullAccess
)

// String returns the string representation of a permission mode.
func (m Mode) String() string {
	switch m {
	case ReadOnly:
		return "read-only"
	case WorkspaceWrite:
		return "workspace-write"
	case DangerFullAccess:
		return "danger-full-access"
	default:
		return "unknown"
	}
}

// ParseMode parses a mode string.
func ParseMode(s string) Mode {
	switch s {
	case "read-only", "readonly", "plan":
		return ReadOnly
	case "workspace-write", "write":
		return WorkspaceWrite
	case "danger-full-access", "full", "danger":
		return DangerFullAccess
	default:
		return ReadOnly
	}
}

// Allows checks if this mode permits the required mode level.
func (m Mode) Allows(required Mode) bool {
	return m >= required
}
