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
	Model       string   `json:"model,omitempty"`
	MaxTokens   int      `json:"maxTokens,omitempty"`
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

	// LLM-based summarization for compaction (uses API call; default false)
	LLMSummarizationEnabled bool `json:"llmSummarizationEnabled,omitempty"`

	// Model aliases (user-defined short names → full model IDs)
	Aliases map[string]string `json:"aliases,omitempty"`

	// Parallel tool execution settings
	Parallel ParallelConfig `json:"parallel,omitempty"`

	// Allowed directories for VFS (in addition to temp dir and workspace root)
	AllowedDirectories []string `json:"allowedDirectories,omitempty"`

	// Provider capability overrides (e.g., force caching on/off).
	ProviderCapabilities *ProviderCapabilitiesConfig `json:"providerCapabilities,omitempty"`

	// Observability settings (OTEL + Prometheus stack)
	Observability *ObservabilityConfig `json:"observability,omitempty"`

	// NATS server settings
	NATS *NATSConfig `json:"nats,omitempty"`

	// Chat hub settings
	Chat *ChatConfig `json:"chat,omitempty"`

	// Custom settings (arbitrary key-value pairs from plugins/MCP)
	Custom map[string]any `json:"custom,omitempty"`
}

// ChatConfig controls the embedded NATS-based chat hub and platform bridges.
type ChatConfig struct {
	Enabled  bool                         `json:"enabled"`
	Channels map[string]ChatChannelConfig `json:"channels,omitempty"` // key = channel ID (telegram, discord, etc.)
}

// ChatChannelConfig configures a single chat channel.
type ChatChannelConfig struct {
	Enabled  bool               `json:"enabled"`
	Accounts []ChatAccountEntry `json:"accounts,omitempty"`
}

// ChatAccountEntry holds per-account credentials for a chat channel.
type ChatAccountEntry struct {
	ID      string            `json:"id"`
	Enabled bool              `json:"enabled"`
	Config  map[string]string `json:"config"` // channel-specific keys (bot_token, etc.)
}

// NATSConfig controls the embedded NATS server for distributed messaging.
type NATSConfig struct {
	Enabled    bool   `json:"enabled"`              // start NATS server, default true when serving
	Port       int    `json:"port,omitempty"`       // default 4222
	URL        string `json:"url,omitempty"`        // external NATS URL (when not using embedded)
	Embedded   bool   `json:"embedded,omitempty"`   // use embedded server, default true
	Credential string `json:"credential,omitempty"` // NATS credentials file path
}

// ObservabilityConfig controls OTEL instrumentation and the embedded observability stack.
type ObservabilityConfig struct {
	// OTEL SDK
	Enabled       bool    `json:"enabled"`       // master switch, default false
	CollectorAddr string  `json:"collectorAddr"` // default "127.0.0.1:4317" (embedded collector)
	SampleRate    float64 `json:"sampleRate"`    // default 1.0

	// Embedded server (use --no-otel flag to disable, --port to set port)
	ProxyPort     int    `json:"proxyPort"`     // reverse proxy port, default 58080
	ProxyBindAddr string `json:"proxyBindAddr"` // default "127.0.0.1"

	// Remote gateway
	RemoteWrite []RemoteWriteTarget `json:"remoteWrite,omitempty"`
	Federation  []FederationTarget  `json:"federation,omitempty"`

	// Local persistence under DataDir
	DataDir          string `json:"dataDir"`          // default "~/.ycode/otel"
	LogRetentionDays int    `json:"logRetentionDays"` // default 3
	LogConversations bool   `json:"logConversations"` // log full conversations, default true when enabled
	LogToolDetails   bool   `json:"logToolDetails"`   // log full tool input/output, default true
	PersistTraces    bool   `json:"persistTraces"`    // write traces to disk, default true when enabled
	PersistMetrics   bool   `json:"persistMetrics"`   // write metrics to disk, default true when enabled
}

// RemoteWriteTarget configures a Prometheus remote-write endpoint.
type RemoteWriteTarget struct {
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	BasicAuth *BasicAuth        `json:"basicAuth,omitempty"`
}

// BasicAuth holds username/password for remote-write authentication.
type BasicAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// FederationTarget configures a Prometheus federation upstream.
type FederationTarget struct {
	URL   string   `json:"url"`
	Match []string `json:"match"` // metric selectors
}

// ProviderCapabilitiesConfig lets users override auto-detected provider capabilities.
type ProviderCapabilitiesConfig struct {
	// CachingSupported overrides whether the provider supports prompt caching.
	// nil = use auto-detection, true/false = override.
	CachingSupported *bool `json:"cachingSupported,omitempty"`
}

// ParallelConfig controls concurrent tool execution.
type ParallelConfig struct {
	Enabled     bool `json:"enabled"`     // master switch (default true)
	MaxStandard int  `json:"maxStandard"` // max concurrent standard tools (default 8)
	MaxLLM      int  `json:"maxLLM"`      // max concurrent LLM tools (default 2)
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Model:              "claude-sonnet-4-20250514",
		MaxTokens:          8192,
		PermissionMode:     "ask",
		AutoCompactEnabled: true,
		Parallel: ParallelConfig{
			Enabled:     true,
			MaxStandard: 8,
			MaxLLM:      2,
		},
		Observability: &ObservabilityConfig{
			Enabled:          true,
			LogConversations: true,
		},
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
	if overlay.LLMSummarizationEnabled {
		cfg.LLMSummarizationEnabled = true
	}
	if overlay.Parallel.Enabled {
		cfg.Parallel.Enabled = true
	}
	if overlay.Parallel.MaxStandard != 0 {
		cfg.Parallel.MaxStandard = overlay.Parallel.MaxStandard
	}
	if overlay.Parallel.MaxLLM != 0 {
		cfg.Parallel.MaxLLM = overlay.Parallel.MaxLLM
	}
	if len(overlay.AllowedDirectories) > 0 {
		cfg.AllowedDirectories = append(cfg.AllowedDirectories, overlay.AllowedDirectories...)
	}
	if overlay.Aliases != nil {
		if cfg.Aliases == nil {
			cfg.Aliases = make(map[string]string)
		}
		for k, v := range overlay.Aliases {
			cfg.Aliases[k] = v
		}
	}
	if overlay.ProviderCapabilities != nil {
		cfg.ProviderCapabilities = overlay.ProviderCapabilities
	}
	if overlay.Observability != nil {
		if cfg.Observability == nil {
			cfg.Observability = &ObservabilityConfig{}
		}
		o := overlay.Observability
		if o.Enabled {
			cfg.Observability.Enabled = true
		}
		if o.CollectorAddr != "" {
			cfg.Observability.CollectorAddr = o.CollectorAddr
		}
		if o.SampleRate != 0 {
			cfg.Observability.SampleRate = o.SampleRate
		}
		if o.ProxyPort != 0 {
			cfg.Observability.ProxyPort = o.ProxyPort
		}
		if o.ProxyBindAddr != "" {
			cfg.Observability.ProxyBindAddr = o.ProxyBindAddr
		}
		if len(o.RemoteWrite) > 0 {
			cfg.Observability.RemoteWrite = o.RemoteWrite
		}
		if len(o.Federation) > 0 {
			cfg.Observability.Federation = o.Federation
		}
		if o.DataDir != "" {
			cfg.Observability.DataDir = o.DataDir
		}
		if o.LogRetentionDays != 0 {
			cfg.Observability.LogRetentionDays = o.LogRetentionDays
		}
		if o.LogConversations {
			cfg.Observability.LogConversations = true
		}
		if o.LogToolDetails {
			cfg.Observability.LogToolDetails = true
		}
		if o.PersistTraces {
			cfg.Observability.PersistTraces = true
		}
		if o.PersistMetrics {
			cfg.Observability.PersistMetrics = true
		}
	}
	if overlay.Chat != nil {
		cfg.Chat = overlay.Chat
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
	case "allowedDirectories":
		return c.AllowedDirectories, true
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
