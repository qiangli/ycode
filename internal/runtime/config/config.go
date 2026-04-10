package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds all runtime configuration.
type Config struct {
	// API settings
	Model       string  `json:"model,omitempty"`
	MaxTokens   int     `json:"maxTokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`

	// Permission mode
	PermissionMode string `json:"permissionMode,omitempty"`

	// Memory settings
	AutoMemoryEnabled  bool `json:"autoMemoryEnabled,omitempty"`
	AutoDreamEnabled   bool `json:"autoDreamEnabled,omitempty"`
	AutoCompactEnabled bool `json:"autoCompactEnabled,omitempty"`

	// Session settings
	SessionDir string `json:"sessionDir,omitempty"`

	// File checkpointing
	FileCheckpointingEnabled bool `json:"fileCheckpointingEnabled,omitempty"`

	// Model aliases (user-defined short names → full model IDs)
	Aliases map[string]string `json:"aliases,omitempty"`

	// Custom settings (arbitrary key-value pairs from plugins/MCP)
	Custom map[string]any `json:"custom,omitempty"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Model:              "claude-sonnet-4-20250514",
		MaxTokens:          8192,
		PermissionMode:     "ask",
		AutoCompactEnabled: true,
	}
}

// Loader loads and merges configuration from multiple tiers.
type Loader struct {
	userDir    string // ~/.config/ycode/
	projectDir string // .ycode/ in project root
	localDir   string // .ycode/ in CWD
}

// NewLoader creates a config loader.
func NewLoader(userDir, projectDir, localDir string) *Loader {
	return &Loader{
		userDir:    userDir,
		projectDir: projectDir,
		localDir:   localDir,
	}
}

// Load loads and merges config from all tiers: user < project < local.
func (l *Loader) Load() (*Config, error) {
	cfg := DefaultConfig()

	// Load tiers in order (later overrides earlier).
	// settings.local.json is the highest-priority tier (typically gitignored).
	tiers := []string{
		filepath.Join(l.userDir, "settings.json"),
		filepath.Join(l.projectDir, "settings.json"),
		filepath.Join(l.localDir, "settings.json"),
		filepath.Join(l.localDir, "settings.local.json"),
	}

	for _, path := range tiers {
		if err := mergeFromFile(cfg, path); err != nil {
			return nil, fmt.Errorf("load config %s: %w", path, err)
		}
	}

	return cfg, nil
}

// mergeFromFile reads a JSON file and merges non-zero values into cfg.
func mergeFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var overlay Config
	if err := json.Unmarshal(data, &overlay); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	// Merge non-zero fields.
	if overlay.Model != "" {
		cfg.Model = overlay.Model
	}
	if overlay.MaxTokens != 0 {
		cfg.MaxTokens = overlay.MaxTokens
	}
	if overlay.Temperature != nil {
		cfg.Temperature = overlay.Temperature
	}
	if overlay.PermissionMode != "" {
		cfg.PermissionMode = overlay.PermissionMode
	}
	if overlay.SessionDir != "" {
		cfg.SessionDir = overlay.SessionDir
	}
	if overlay.AutoMemoryEnabled {
		cfg.AutoMemoryEnabled = true
	}
	if overlay.AutoDreamEnabled {
		cfg.AutoDreamEnabled = true
	}
	if overlay.AutoCompactEnabled {
		cfg.AutoCompactEnabled = true
	}
	if overlay.FileCheckpointingEnabled {
		cfg.FileCheckpointingEnabled = true
	}
	if overlay.Aliases != nil {
		if cfg.Aliases == nil {
			cfg.Aliases = make(map[string]string)
		}
		for k, v := range overlay.Aliases {
			cfg.Aliases[k] = v
		}
	}
	if overlay.Custom != nil {
		if cfg.Custom == nil {
			cfg.Custom = make(map[string]any)
		}
		for k, v := range overlay.Custom {
			cfg.Custom[k] = v
		}
	}

	return nil
}

// Get returns a config value by key.
func (c *Config) Get(key string) (any, bool) {
	switch key {
	case "model":
		return c.Model, true
	case "maxTokens":
		return c.MaxTokens, true
	case "permissionMode":
		return c.PermissionMode, true
	case "autoMemoryEnabled":
		return c.AutoMemoryEnabled, true
	case "autoCompactEnabled":
		return c.AutoCompactEnabled, true
	case "aliases":
		return c.Aliases, true
	default:
		if c.Custom != nil {
			v, ok := c.Custom[key]
			return v, ok
		}
		return nil, false
	}
}

// Set sets a config value by key.
func (c *Config) Set(key string, value any) {
	switch key {
	case "model":
		if s, ok := value.(string); ok {
			c.Model = s
		}
	case "maxTokens":
		if n, ok := value.(float64); ok {
			c.MaxTokens = int(n)
		}
	case "permissionMode":
		if s, ok := value.(string); ok {
			c.PermissionMode = s
		}
	default:
		if c.Custom == nil {
			c.Custom = make(map[string]any)
		}
		c.Custom[key] = value
	}
}

// GetLocalConfigField reads a single field from a JSON config file.
// Returns (nil, false) if the file or field doesn't exist.
func GetLocalConfigField(path string, key string) (any, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

// SetLocalConfigField sets a single field in a JSON config file.
// If value is nil, the field is removed. Creates the file and parent dirs if needed.
// Other fields in the file are preserved.
func SetLocalConfigField(path string, key string, value any) error {
	existing := make(map[string]any)
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &existing)
	} else if !os.IsNotExist(err) {
		return err
	}

	if value == nil {
		delete(existing, key)
	} else {
		existing[key] = value
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}
