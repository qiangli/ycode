package memory

import "time"

// Type identifies the category of a memory.
type Type string

const (
	TypeUser       Type = "user"
	TypeFeedback   Type = "feedback"
	TypeProject    Type = "project"
	TypeReference  Type = "reference"
	TypeEpisodic   Type = "episodic"   // specific agent experiences with temporal context
	TypeProcedural Type = "procedural" // workflow patterns, decision-making heuristics
	TypeTask       Type = "task"       // persistent structured task state
)

// Scope determines where a memory is stored.
type Scope string

const (
	// ScopeGlobal stores memories in ~/.agents/ycode/memory/ (shared across all projects).
	ScopeGlobal Scope = "global"
	// ScopeProject stores memories in ~/.agents/ycode/projects/{hash}/memory/ (project-specific).
	ScopeProject Scope = "project"
	// ScopeTeam stores memories shared across team members.
	ScopeTeam Scope = "team"
	// ScopeUser stores memories private to a single user.
	ScopeUser Scope = "user"
)

// Memory represents a single persisted memory.
type Memory struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Type        Type    `json:"type"`
	Scope       Scope   `json:"scope,omitempty"` // defaults to ScopeProject if empty
	Content     string  `json:"content"`
	FilePath    string  `json:"file_path,omitempty"`
	Importance  float64 `json:"importance,omitempty"` // 0.0-1.0, used in composite recall scoring (default 0.5)
	ScopePath   string  `json:"scope_path,omitempty"` // hierarchical scope path, e.g., "team/backend"

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EffectiveScope returns the memory's scope, defaulting to ScopeProject.
func (m *Memory) EffectiveScope() Scope {
	if m.Scope == "" {
		return ScopeProject
	}
	return m.Scope
}

// Frontmatter is the YAML frontmatter at the top of a memory file.
type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        Type   `yaml:"type"`
	Scope       Scope  `yaml:"scope,omitempty"`
}
