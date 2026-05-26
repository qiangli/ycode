package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	PersonaEnabled     bool `json:"personaEnabled,omitempty"` // tailored user experience via persona

	// Session settings
	SessionDir string `json:"sessionDir,omitempty"`

	// File checkpointing
	FileCheckpointingEnabled bool `json:"fileCheckpointingEnabled,omitempty"`

	// LLM-based summarization for compaction (uses API call; default false)
	LLMSummarizationEnabled bool `json:"llmSummarizationEnabled,omitempty"`

	// Weak model for cheap secondary tasks (summarization, commit messages).
	// When set, LLM summarization tries this model first, falling back to the
	// main model on failure. Examples: "claude-haiku-4-5-20251001", "gpt-4o-mini".
	WeakModel string `json:"weakModel,omitempty"`

	// Cache warming: background pings to keep Anthropic prompt cache alive.
	// Only effective when the provider supports prompt caching.
	CacheWarmingEnabled bool `json:"cacheWarmingEnabled,omitempty"`

	// Model aliases (user-defined short names → full model IDs)
	Aliases map[string]string `json:"aliases,omitempty"`

	// Parallel tool execution settings
	Parallel ParallelConfig `json:"parallel,omitempty"`

	// Additional instruction file paths. Each entry can be:
	//   - Absolute path: /path/to/instructions.md
	//   - Relative path: docs/INSTRUCTIONS.md (resolved from project root)
	//   - Home-relative: ~/custom/instructions.md
	//   - URL: https://example.com/instructions.md
	Instructions []string `json:"instructions,omitempty"`

	// Allowed directories for VFS (in addition to temp dir and workspace root)
	AllowedDirectories []string `json:"allowedDirectories,omitempty"`

	// Provider capability overrides (e.g., force caching on/off).
	ProviderCapabilities *ProviderCapabilitiesConfig `json:"providerCapabilities,omitempty"`

	// Observability settings (OTEL + Prometheus stack)
	Observability *ObservabilityConfig `json:"observability,omitempty"`

	// Local inference engine settings (Ollama-based)
	Inference *InferenceConfig `json:"inference,omitempty"`

	// NATS server settings
	NATS *NATSConfig `json:"nats,omitempty"`

	// Chat hub settings
	Chat *ChatConfig `json:"chat,omitempty"`

	// Container isolation settings (Podman-based)
	Container *ContainerConfig `json:"container,omitempty"`

	// Embedded git server settings (Gitea-based)
	GitServer *GitServerConfig `json:"gitServer,omitempty"`

	// Multi-agent collaboration task queue settings (see docs/agent-collab.md).
	Tasks *TasksConfig `json:"tasks,omitempty"`

	// Browser automation backend selection (see internal/runtime/mcpservers).
	Browser *BrowserConfig `json:"browser,omitempty"`

	// Self-heal observer (see internal/runtime/selfheal). Phase 1
	// of the autonomous fix loop: watch tool-call spans for
	// ycode-bug-shaped failures and write them to a JSONL log. On
	// by default per the "intrinsic feature, opt-out" convention;
	// explicit false disables.
	SelfHeal *SelfHealConfig `json:"selfHeal,omitempty"`

	// Project identity for OTEL attribution. Empty fields fall back
	// to auto-detection (git remote, cwd basename). See
	// internal/runtime/origin.
	Project *ProjectConfig `json:"project,omitempty"`

	// Toolsets maps user-defined toolset names to tool names.
	Toolsets map[string][]string `json:"toolsets,omitempty"`

	// Personality settings
	Personality string `json:"personality,omitempty"` // builtin personality name (e.g., "pirate", "stern")
	SOULFile    string `json:"soul_file,omitempty"`   // path to SOUL.md for custom identity

	// Custom settings (arbitrary key-value pairs from plugins/MCP)
	Custom map[string]any `json:"custom,omitempty"`
}

// ChatConfig controls the embedded NATS-based chat hub and platform bridges.
//
// On by default per ycode's "intrinsic feature, opt-out" convention.
// Without configured Channels the hub is a no-op at runtime; flipping
// the master switch off skips the wiring entirely.
type ChatConfig struct {
	Enabled  *bool                        `json:"enabled,omitempty"`
	Channels map[string]ChatChannelConfig `json:"channels,omitempty"` // key = channel ID (telegram, discord, etc.)
}

// IsEnabled — nil receiver and nil Enabled both return true (default
// on). Only explicit false opts out.
func (c *ChatConfig) IsEnabled() bool {
	if c == nil || c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// ChatChannelConfig configures a single chat channel.
// Individual channels and accounts are off by default — they require
// credentials, so enabling them without config would be useless. This
// is the one exception to the "on by default" convention.
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
//
// On by default. Runtime startup is in-process and cheap; even
// unused, NATS is the bus substrate for agent collab.
type NATSConfig struct {
	Enabled    *bool  `json:"enabled,omitempty"`
	Port       int    `json:"port,omitempty"`       // default 4222
	URL        string `json:"url,omitempty"`        // external NATS URL (when not using embedded)
	Embedded   bool   `json:"embedded,omitempty"`   // use embedded server, default true
	Credential string `json:"credential,omitempty"` // NATS credentials file path
}

// IsEnabled — nil receiver and nil Enabled both return true (default on).
func (c *NATSConfig) IsEnabled() bool {
	if c == nil || c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// SelfHealConfig controls the Phase 1 selfheal observer (and, in
// later phases, the full autoloop fix pipeline). On by default per
// the user's confirmed scope (opt-out via `selfHeal.enabled: false`).
// Phase 1 only writes JSONL observations; later phases will read
// other fields to drive the worker pool / PR creation.
type SelfHealConfig struct {
	Enabled  *bool  `json:"enabled,omitempty"`
	SinkPath string `json:"sinkPath,omitempty"` // override the default ~/.agents/ycode/selfheal/observations.jsonl
}

// IsEnabled — nil receiver and nil Enabled both return true. The
// YCODE_SELFHEAL_DISABLE=1 env var is a hard override that wins
// over the config: selfheal workers spawn child ycode processes
// with this set so the child's selfheal observer doesn't try to
// "fix" failures the parent worker itself caused.
func (c *SelfHealConfig) IsEnabled() bool {
	if v := strings.TrimSpace(os.Getenv("YCODE_SELFHEAL_DISABLE")); v == "1" || strings.EqualFold(v, "true") {
		return false
	}
	if c == nil || c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// ObservabilityConfig controls OTEL instrumentation and the embedded observability stack.
//
// Convention: ycode features are ON by default and opt-out. The
// `Enabled` field is a *bool so the JSON loader can distinguish
// "absent" (nil → on) from "explicitly false" (pointer to false →
// off). Use IsEnabled() at every read site rather than checking the
// pointer directly.
type ObservabilityConfig struct {
	// OTEL SDK — on by default. Set `observability.enabled: false`
	// in settings.json to disable.
	Enabled       *bool   `json:"enabled,omitempty"`
	CollectorAddr string  `json:"collectorAddr"` // default "127.0.0.1:4317" (embedded collector)
	SampleRate    float64 `json:"sampleRate"`    // default 1.0

	// Embedded server (use --no-otel flag to disable, --port to set port)
	ProxyPort     int    `json:"proxyPort"`     // reverse proxy port, default selfinit.DefaultPort (31415)
	ProxyBindAddr string `json:"proxyBindAddr"` // default "127.0.0.1"

	// OTLP ingress ports — pinned to well-known defaults so any third-party
	// OTLP client can publish to ycode without configuration. Set to a
	// non-zero value to override; set to a negative value to opt back into
	// ephemeral allocation.
	OTLPGRPCPort int `json:"otlpGRPCPort,omitempty"` // default 4317
	OTLPHTTPPort int `json:"otlpHTTPPort,omitempty"` // default 4318

	// Remote gateway
	RemoteWrite []RemoteWriteTarget `json:"remoteWrite,omitempty"`
	Federation  []FederationTarget  `json:"federation,omitempty"`

	// Local persistence under DataDir
	DataDir          string `json:"dataDir"`          // default "~/.agents/ycode/otel"
	LogRetentionDays int    `json:"logRetentionDays"` // default 3
	LogConversations bool   `json:"logConversations"` // log full conversations, default true when enabled
	LogToolDetails   bool   `json:"logToolDetails"`   // log full tool input/output, default true
	PersistTraces    bool   `json:"persistTraces"`    // write traces to disk, default true when enabled
	PersistMetrics   bool   `json:"persistMetrics"`   // write metrics to disk, default true when enabled
	PersistLogs      bool   `json:"persistLogs"`      // write structured logs to disk, default true when enabled

	// Auto-start: fork a detached `ycode serve` if no collector is running.
	AutoPulse bool `json:"autoPulse,omitempty"`
}

// IsEnabled reports whether the observability stack is on. Treats nil
// (the absent / never-set case) as enabled — that's the ycode default.
// Returns false only when the user explicitly set
// `observability.enabled: false` in settings.json.
func (c *ObservabilityConfig) IsEnabled() bool {
	if c == nil {
		return true
	}
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// InferenceConfig controls the embedded Ollama-based inference engine.
//
// On by default. The runner is part of ycode's agent-OS substrate;
// without it `bin/ycode model use ...` and any local-model workflow
// degrade. Opt-out: set `inference.enabled: false` in settings.json.
type InferenceConfig struct {
	Enabled      *bool  `json:"enabled,omitempty"`
	DefaultModel string `json:"defaultModel,omitempty"` // pre-load model on startup
	ModelsDir    string `json:"modelsDir,omitempty"`    // model storage directory
	GPULayers    int    `json:"gpuLayers,omitempty"`    // GPU offload layers (-1 = auto)
	MaxVRAMMB    int    `json:"maxVramMB,omitempty"`    // limit GPU memory usage

	// Gateway controls the per-process localhost gateway that fronts
	// ollama. When Mode is "remote", outbound calls are proxied through
	// cloudbox to a remote ollama running on another machine; agents and
	// tools that read OLLAMA_HOST see the same local URL either way.
	// Omit / leave Mode empty to default to "embedded".
	Gateway InferenceGatewayConfig `json:"gateway,omitempty"`
}

// InferenceGatewayConfig is the per-process localhost gateway settings
// for ollama. URL + TokenFile are only consulted when Mode == "remote".
type InferenceGatewayConfig struct {
	Mode      string `json:"mode,omitempty"`      // "embedded" (default) | "remote"
	URL       string `json:"url,omitempty"`       // https://cloudbox/h/<host>/app/ollama/
	TokenFile string `json:"tokenFile,omitempty"` // path to file containing the cloudbox Bearer token
}

// IsEnabled — nil receiver and nil Enabled both return true (default on).
func (c *InferenceConfig) IsEnabled() bool {
	if c == nil || c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// ContainerConfig controls the embedded Podman-based container isolation engine.
//
// On by default. Sandbox isolation for the agent's bash tool, yc
// sandbox, browser containers, and the SearXNG side-service all
// depend on it. Degrades gracefully when podman is missing on the
// host (warns + continues without sandbox). Opt-out:
// `container.enabled: false`.
type ContainerConfig struct {
	Enabled      *bool  `json:"enabled,omitempty"`
	SocketPath   string `json:"socketPath,omitempty"`   // explicit podman socket path
	Image        string `json:"image,omitempty"`        // default sandbox image (default: ycode-sandbox:latest)
	Network      string `json:"network,omitempty"`      // network mode: "bridge" (default), "host", "none"
	ReadOnlyRoot bool   `json:"readOnlyRoot,omitempty"` // read-only root filesystem (default true)
	PoolSize     int    `json:"poolSize,omitempty"`     // warm pool size (0 = no pool)
	CPUs         string `json:"cpus,omitempty"`         // per-container CPU limit (e.g., "2.0")
	Memory       string `json:"memory,omitempty"`       // per-container memory limit (e.g., "4g")

	// Gateway controls the per-process localhost gateway that fronts the
	// container engine. When Mode is "remote", podman/docker requests are
	// proxied through cloudbox to a remote engine on another machine;
	// agents and tools that read DOCKER_HOST/CONTAINER_HOST see the same
	// local socket either way. Omit / leave Mode empty to default to
	// "embedded".
	Gateway ContainerGatewayConfig `json:"gateway,omitempty"`
}

// ContainerGatewayConfig is the per-process localhost gateway settings
// for podman. URL + TokenFile are only consulted when Mode == "remote".
type ContainerGatewayConfig struct {
	Mode      string `json:"mode,omitempty"`      // "embedded" (default) | "remote"
	URL       string `json:"url,omitempty"`       // https://cloudbox/h/<host>/app/podman/
	TokenFile string `json:"tokenFile,omitempty"` // path to file containing the cloudbox Bearer token
}

// IsEnabled — nil receiver and nil Enabled both return true (default on).
func (c *ContainerConfig) IsEnabled() bool {
	if c == nil || c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GitServerConfig controls the embedded Gitea-based git server for agent swarm coordination.
//
// On by default. Foreman backlog reconciler, loom worker coordination,
// gitea-mcp, and multi-agent task queue all rely on it. Opt-out:
// `gitServer.enabled: false`.
type GitServerConfig struct {
	Enabled  *bool  `json:"enabled,omitempty"`
	DataDir  string `json:"dataDir,omitempty"`  // data directory (default: ~/.agents/ycode/gitea)
	AppName  string `json:"appName,omitempty"`  // display name (default: "ycode Git")
	HTTPOnly bool   `json:"httpOnly,omitempty"` // disable SSH access (default true)
	Token    string `json:"token,omitempty"`    // admin API token (auto-generated if empty)
}

// IsEnabled — nil receiver and nil Enabled both return true (default on).
func (c *GitServerConfig) IsEnabled() bool {
	if c == nil || c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// ProjectConfig declares stable identity for the current ycode
// workspace. Both fields default to the auto-resolved values when
// empty (see internal/runtime/origin). Useful for CI / multi-repo
// setups where the auto-detected value isn't what you want in
// dashboards.
type ProjectConfig struct {
	ID   string `json:"id,omitempty"`   // stable identifier, e.g. github.com/foo/bar
	Name string `json:"name,omitempty"` // human-readable name shown in dashboards
}

// BrowserConfig selects which ycode-native browser mode handles the
// browser_* tools and tunes per-mode behavior. See
// internal/runtime/mcpservers for the live / probe / solo
// implementations.
type BrowserConfig struct {
	Mode string `json:"mode,omitempty"` // "live" | "probe" | "solo"

	// live (Chrome extension over WebSocket)
	LivePort int `json:"livePort,omitempty"` // default 58082

	// probe (CDP attach)
	ProbeURL string `json:"probeURL,omitempty"` // default http://localhost:9222

	// solo (chromedp launches fresh Chrome)
	SoloChromePath  string `json:"soloChromePath,omitempty"`  // empty → auto-detect / podman fallback
	SoloHeaded      bool   `json:"soloHeaded,omitempty"`      // default headless
	SoloUserDataDir string `json:"soloUserDataDir,omitempty"` // empty → ephemeral

	// Reliability layer toggles (openchrome-inspired). nil = default
	// on; explicit false disables. See
	// internal/runtime/mcpservers/reliability.
	HintEngine     *bool `json:"hintEngine,omitempty"`
	RalphFallback  *bool `json:"ralphFallback,omitempty"`
	CircuitBreaker *bool `json:"circuitBreaker,omitempty"`
	CompactDOM     *bool `json:"compactDOM,omitempty"`
	PatternLearner *bool `json:"patternLearner,omitempty"`
}

// TasksConfig controls the multi-agent collaboration task queue.
// See docs/agent-collab.md.
type TasksConfig struct {
	// CICommand is the shell command run by the merger in an isolated
	// checkout of each PR's prospective merge commit. Empty = no CI gate
	// (auto-merge unconditionally; only safe for trusted setups).
	CICommand string `json:"ciCommand,omitempty"`
	// CITimeoutSeconds caps how long CICommand may run. 0 = 1800 (30 min).
	CITimeoutSeconds int `json:"ciTimeoutSeconds,omitempty"`
	// AutoMerge enables the merger loop. When false, PRs accumulate but
	// are not auto-merged — useful for human review.
	AutoMerge bool `json:"autoMerge,omitempty"`
	// PollSeconds is how often the merger checks for new PRs. 0 = 10s.
	PollSeconds int `json:"pollSeconds,omitempty"`
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
	MaxAgent    int  `json:"maxAgent"`    // max concurrent agent spawns (default 4)
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Model:              "claude-sonnet-4-20250514",
		MaxTokens:          8192,
		PermissionMode:     "ask",
		AutoCompactEnabled: true,
		PersonaEnabled:     true,
		Parallel: ParallelConfig{
			Enabled:     true,
			MaxStandard: 8,
			MaxLLM:      2,
			MaxAgent:    4,
		},
		Observability: &ObservabilityConfig{
			// Enabled is nil → IsEnabled() returns true (ycode default).
			// Set `observability.enabled: false` in settings.json to opt out.
			LogConversations: true,
			LogToolDetails:   true,
			PersistTraces:    true,
			PersistMetrics:   true,
			PersistLogs:      true,
		},
		GitServer: &GitServerConfig{
			// Enabled is nil → IsEnabled() returns true (ycode default).
			// Set `gitserver.enabled: false` in settings.json to opt out.
			HTTPOnly: true,
		},
		// Every default-on sub-config needs a non-nil pointer at this
		// stage. Without it, IsEnabled() returns true (nil-safe default)
		// but consumers that read fields off the sub-config (SocketPath,
		// ModelsDir, etc.) panic on nil deref. Mirrors the Observability
		// + GitServer treatment above.
		Inference: &InferenceConfig{},
		Container: &ContainerConfig{},
		NATS:      &NATSConfig{},
		Chat:      &ChatConfig{},
	}
}

// Loader loads and merges configuration from multiple tiers.
//
// Tiers, in order of increasing precedence (later overrides earlier):
//
//  1. userDir        — ~/.config/ycode/                       (user-global, all projects)
//  2. perProjectDir  — ~/.agents/ycode/projects/<id>/         (user-global, this project across checkouts)
//  3. projectDir     — <cwd>/.agents/ycode/                   (team-shared via git)
//  4. localDir       — <cwd>/.agents/ycode/                   (per-checkout settings.local.json)
//
// perProjectDir is optional; empty string skips that tier. It is keyed
// by the logical project id from internal/runtime/projectid so two
// checkouts of the same repo converge on shared settings without
// committing them through git.
type Loader struct {
	userDir       string
	perProjectDir string
	projectDir    string
	localDir      string
}

// NewLoader creates a config loader with three tiers (user/project/local).
// Callers that want the per-project tier should use NewLoaderWithPerProject.
func NewLoader(userDir, projectDir, localDir string) *Loader {
	return &Loader{
		userDir:    userDir,
		projectDir: projectDir,
		localDir:   localDir,
	}
}

// NewLoaderWithPerProject creates a four-tier loader. perProjectDir is
// the resolved ~/.agents/ycode/projects/<id>/ directory; empty disables
// the tier.
func NewLoaderWithPerProject(userDir, perProjectDir, projectDir, localDir string) *Loader {
	return &Loader{
		userDir:       userDir,
		perProjectDir: perProjectDir,
		projectDir:    projectDir,
		localDir:      localDir,
	}
}

// Load loads and merges config from all configured tiers.
func (l *Loader) Load() (*Config, error) {
	cfg := DefaultConfig()

	tiers := []string{
		filepath.Join(l.userDir, "settings.json"),
	}
	if l.perProjectDir != "" {
		tiers = append(tiers, filepath.Join(l.perProjectDir, "settings.json"))
	}
	tiers = append(tiers,
		filepath.Join(l.projectDir, "settings.json"),
		filepath.Join(l.localDir, "settings.json"),
		filepath.Join(l.localDir, "settings.local.json"),
	)

	for _, path := range tiers {
		if err := mergeFromFile(cfg, path); err != nil {
			return nil, fmt.Errorf("load config %s: %w", path, err)
		}
	}

	// Apply YCODE_<UPPER_SNAKE_PATH> env-var overrides over the merged
	// JSON tiers. Higher precedence than any config file, lower
	// precedence than CLI flags (applied later in cmd/ycode/main.go).
	// See env.go for the contract and safeguards.
	_ = ApplyEnvOverrides(cfg)

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
	if overlay.WeakModel != "" {
		cfg.WeakModel = overlay.WeakModel
	}
	if overlay.CacheWarmingEnabled {
		cfg.CacheWarmingEnabled = true
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
	if overlay.Parallel.MaxAgent != 0 {
		cfg.Parallel.MaxAgent = overlay.Parallel.MaxAgent
	}
	if len(overlay.Instructions) > 0 {
		cfg.Instructions = append(cfg.Instructions, overlay.Instructions...)
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
		if o.Enabled != nil {
			// Copy the pointer wholesale so explicit `false` propagates
			// (the whole point of the *bool shape — letting users opt
			// out of the default-on behavior).
			cfg.Observability.Enabled = o.Enabled
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
		if o.OTLPGRPCPort != 0 {
			cfg.Observability.OTLPGRPCPort = o.OTLPGRPCPort
		}
		if o.OTLPHTTPPort != 0 {
			cfg.Observability.OTLPHTTPPort = o.OTLPHTTPPort
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
		if o.PersistLogs {
			cfg.Observability.PersistLogs = true
		}
	}
	if overlay.Chat != nil {
		cfg.Chat = overlay.Chat
	}
	if overlay.Inference != nil {
		if cfg.Inference == nil {
			cfg.Inference = &InferenceConfig{}
		}
		in := overlay.Inference
		if in.Enabled != nil {
			cfg.Inference.Enabled = in.Enabled
		}
		if in.DefaultModel != "" {
			cfg.Inference.DefaultModel = in.DefaultModel
		}
		if in.ModelsDir != "" {
			cfg.Inference.ModelsDir = in.ModelsDir
		}
		if in.GPULayers != 0 {
			cfg.Inference.GPULayers = in.GPULayers
		}
		if in.MaxVRAMMB != 0 {
			cfg.Inference.MaxVRAMMB = in.MaxVRAMMB
		}
	}
	if overlay.NATS != nil {
		if cfg.NATS == nil {
			cfg.NATS = &NATSConfig{}
		}
		n := overlay.NATS
		if n.Enabled != nil {
			cfg.NATS.Enabled = n.Enabled
		}
		if n.Port != 0 {
			cfg.NATS.Port = n.Port
		}
		if n.URL != "" {
			cfg.NATS.URL = n.URL
		}
		if n.Embedded {
			cfg.NATS.Embedded = true
		}
		if n.Credential != "" {
			cfg.NATS.Credential = n.Credential
		}
	}
	if overlay.Container != nil {
		if cfg.Container == nil {
			cfg.Container = &ContainerConfig{}
		}
		co := overlay.Container
		if co.Enabled != nil {
			cfg.Container.Enabled = co.Enabled
		}
		if co.SocketPath != "" {
			cfg.Container.SocketPath = co.SocketPath
		}
		if co.Image != "" {
			cfg.Container.Image = co.Image
		}
		if co.Network != "" {
			cfg.Container.Network = co.Network
		}
		if co.ReadOnlyRoot {
			cfg.Container.ReadOnlyRoot = true
		}
		if co.PoolSize != 0 {
			cfg.Container.PoolSize = co.PoolSize
		}
		if co.CPUs != "" {
			cfg.Container.CPUs = co.CPUs
		}
		if co.Memory != "" {
			cfg.Container.Memory = co.Memory
		}
	}
	if overlay.GitServer != nil {
		if cfg.GitServer == nil {
			cfg.GitServer = &GitServerConfig{}
		}
		gs := overlay.GitServer
		if gs.Enabled != nil {
			cfg.GitServer.Enabled = gs.Enabled
		}
		if gs.DataDir != "" {
			cfg.GitServer.DataDir = gs.DataDir
		}
		if gs.AppName != "" {
			cfg.GitServer.AppName = gs.AppName
		}
		if gs.HTTPOnly {
			cfg.GitServer.HTTPOnly = true
		}
		if gs.Token != "" {
			cfg.GitServer.Token = gs.Token
		}
	}
	if overlay.Tasks != nil {
		if cfg.Tasks == nil {
			cfg.Tasks = &TasksConfig{}
		}
		tk := overlay.Tasks
		if tk.CICommand != "" {
			cfg.Tasks.CICommand = tk.CICommand
		}
		if tk.CITimeoutSeconds != 0 {
			cfg.Tasks.CITimeoutSeconds = tk.CITimeoutSeconds
		}
		if tk.AutoMerge {
			cfg.Tasks.AutoMerge = true
		}
		if tk.PollSeconds != 0 {
			cfg.Tasks.PollSeconds = tk.PollSeconds
		}
	}
	if overlay.Project != nil {
		if cfg.Project == nil {
			cfg.Project = &ProjectConfig{}
		}
		if overlay.Project.ID != "" {
			cfg.Project.ID = overlay.Project.ID
		}
		if overlay.Project.Name != "" {
			cfg.Project.Name = overlay.Project.Name
		}
	}
	if overlay.Browser != nil {
		if cfg.Browser == nil {
			cfg.Browser = &BrowserConfig{}
		}
		b := overlay.Browser
		if b.Mode != "" {
			cfg.Browser.Mode = b.Mode
		}
		if b.LivePort != 0 {
			cfg.Browser.LivePort = b.LivePort
		}
		if b.ProbeURL != "" {
			cfg.Browser.ProbeURL = b.ProbeURL
		}
		if b.SoloChromePath != "" {
			cfg.Browser.SoloChromePath = b.SoloChromePath
		}
		if b.SoloHeaded {
			cfg.Browser.SoloHeaded = true
		}
		if b.SoloUserDataDir != "" {
			cfg.Browser.SoloUserDataDir = b.SoloUserDataDir
		}
		if b.HintEngine != nil {
			cfg.Browser.HintEngine = b.HintEngine
		}
		if b.RalphFallback != nil {
			cfg.Browser.RalphFallback = b.RalphFallback
		}
		if b.CircuitBreaker != nil {
			cfg.Browser.CircuitBreaker = b.CircuitBreaker
		}
		if b.CompactDOM != nil {
			cfg.Browser.CompactDOM = b.CompactDOM
		}
		if b.PatternLearner != nil {
			cfg.Browser.PatternLearner = b.PatternLearner
		}
	}
	if overlay.Toolsets != nil {
		if cfg.Toolsets == nil {
			cfg.Toolsets = make(map[string][]string)
		}
		for k, v := range overlay.Toolsets {
			cfg.Toolsets[k] = v
		}
	}
	if overlay.Personality != "" {
		cfg.Personality = overlay.Personality
	}
	if overlay.SOULFile != "" {
		cfg.SOULFile = overlay.SOULFile
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
