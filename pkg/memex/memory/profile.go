package memory

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const profileFileName = "_profile.md"

// UserProfile represents a structured user profile built from conversations.
type UserProfile struct {
	BasicInfo    map[string]string `json:"basic_info"`
	Preferences  map[string]string `json:"preferences"`
	Expertise    []string          `json:"expertise"`
	WorkPatterns []string          `json:"work_patterns"`
	LastUpdated  time.Time         `json:"last_updated"`
}

// NewUserProfile creates an empty profile.
func NewUserProfile() *UserProfile {
	return &UserProfile{
		BasicInfo:   make(map[string]string),
		Preferences: make(map[string]string),
	}
}

// LoadProfile loads the user profile from a memory store.
// Returns an empty profile if none exists.
func LoadProfile(store *Store) (*UserProfile, error) {
	path := filepath.Join(store.Dir(), profileFileName)
	mem, err := store.Load(path)
	if err != nil {
		return NewUserProfile(), nil // no profile yet
	}

	return parseProfile(mem.Content), nil
}

// Save persists the profile as a markdown file in the store.
func (p *UserProfile) Save(store *Store) error {
	p.LastUpdated = time.Now()

	mem := &Memory{
		Name:        "_profile",
		Description: "Structured user profile",
		Type:        TypeUser,
		Content:     p.Format(),
		FilePath:    filepath.Join(store.Dir(), profileFileName),
	}

	return store.Save(mem)
}

// Update sets a profile field. Key format: "section.field" (e.g., "basic_info.name").
// For list sections (expertise, work_patterns), the value is appended.
func (p *UserProfile) Update(key, value string) {
	section, field, ok := strings.Cut(key, ".")
	if !ok {
		// No dot — check if the key itself is a section name (for list sections).
		switch key {
		case "expertise":
			p.Expertise = appendUnique(p.Expertise, value)
		case "work_patterns":
			p.WorkPatterns = appendUnique(p.WorkPatterns, value)
		default:
			// Default to basic_info.
			p.BasicInfo[key] = value
		}
		return
	}

	switch section {
	case "basic_info":
		p.BasicInfo[field] = value
	case "preferences":
		p.Preferences[field] = value
	case "expertise":
		p.Expertise = appendUnique(p.Expertise, field)
	case "work_patterns":
		p.WorkPatterns = appendUnique(p.WorkPatterns, field)
	}
}

// Get retrieves a profile field.
func (p *UserProfile) Get(key string) string {
	section, field, ok := strings.Cut(key, ".")
	if !ok {
		return p.BasicInfo[key]
	}

	switch section {
	case "basic_info":
		return p.BasicInfo[field]
	case "preferences":
		return p.Preferences[field]
	}
	return ""
}

// Format renders the profile as human-readable markdown.
func (p *UserProfile) Format() string {
	var b strings.Builder

	if len(p.BasicInfo) > 0 {
		b.WriteString("## Basic Info\n")
		for _, k := range sortedKeys(p.BasicInfo) {
			fmt.Fprintf(&b, "- **%s**: %s\n", k, p.BasicInfo[k])
		}
		b.WriteString("\n")
	}

	if len(p.Preferences) > 0 {
		b.WriteString("## Preferences\n")
		for _, k := range sortedKeys(p.Preferences) {
			fmt.Fprintf(&b, "- **%s**: %s\n", k, p.Preferences[k])
		}
		b.WriteString("\n")
	}

	if len(p.Expertise) > 0 {
		b.WriteString("## Expertise\n")
		for _, e := range p.Expertise {
			fmt.Fprintf(&b, "- %s\n", e)
		}
		b.WriteString("\n")
	}

	if len(p.WorkPatterns) > 0 {
		b.WriteString("## Work Patterns\n")
		for _, w := range p.WorkPatterns {
			fmt.Fprintf(&b, "- %s\n", w)
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

// IsEmpty returns true if the profile has no data.
func (p *UserProfile) IsEmpty() bool {
	return len(p.BasicInfo) == 0 && len(p.Preferences) == 0 &&
		len(p.Expertise) == 0 && len(p.WorkPatterns) == 0
}

// parseProfile reconstructs a UserProfile from markdown content.
func parseProfile(content string) *UserProfile {
	p := NewUserProfile()
	if content == "" {
		return p
	}

	var currentSection string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			currentSection = strings.ToLower(strings.TrimPrefix(line, "## "))
			currentSection = strings.ReplaceAll(currentSection, " ", "_")
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		item := strings.TrimPrefix(line, "- ")

		switch currentSection {
		case "basic_info":
			if k, v, ok := parseKV(item); ok {
				p.BasicInfo[k] = v
			}
		case "preferences":
			if k, v, ok := parseKV(item); ok {
				p.Preferences[k] = v
			}
		case "expertise":
			p.Expertise = append(p.Expertise, item)
		case "work_patterns":
			p.WorkPatterns = append(p.WorkPatterns, item)
		}
	}

	return p
}

// parseKV parses "**key**: value" format.
func parseKV(s string) (string, string, bool) {
	s = strings.TrimPrefix(s, "**")
	key, rest, ok := strings.Cut(s, "**")
	if !ok {
		return "", "", false
	}
	value := strings.TrimPrefix(rest, ": ")
	return key, value, true
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
