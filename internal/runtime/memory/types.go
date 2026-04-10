package memory

import "time"

// Type identifies the category of a memory.
type Type string

const (
	TypeUser      Type = "user"
	TypeFeedback  Type = "feedback"
	TypeProject   Type = "project"
	TypeReference Type = "reference"
)

// Memory represents a single persisted memory.
type Memory struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        Type   `json:"type"`
	Content     string `json:"content"`
	FilePath    string `json:"file_path,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Frontmatter is the YAML frontmatter at the top of a memory file.
type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        Type   `yaml:"type"`
}
