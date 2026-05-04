// Package agentexec provides an execution framework for delegating tasks
// to external agentic CLI tools (Claude Code, Codex, Gemini CLI, Aider,
// Cursor, OpenCode, etc.).
//
// Inspired by:
//   - gastown's AgentPresetInfo registry (13+ agents, single source of truth)
//   - ClawTeam's NativeCliAdapter (9+ agents, flag injection)
//   - agent-orchestrator's Agent plugin interface (6 agents, activity detection)
//   - ralph-claude-code's CLI command builder (shell-injection safe)
package agentexec

// AgentPreset defines the CLI metadata for an external agentic tool.
// Each preset maps an agent type to everything needed to spawn, configure,
// communicate with, and detect the health of that agent.
type AgentPreset struct {
	// Name is the agent type identifier (e.g., "claude", "codex", "gemini").
	Name string `json:"name" yaml:"name"`

	// Command is the CLI binary to invoke (e.g., "claude", "codex", "gemini").
	Command string `json:"command" yaml:"command"`

	// ProcessNames lists process names to detect agent liveness via ps
	// (e.g., ["node"] for Claude Code, ["python"] for Aider).
	ProcessNames []string `json:"process_names,omitempty" yaml:"process_names,omitempty"`

	// PromptFlag is the flag to pass a prompt inline (e.g., "-p" for Claude, "--prompt" for others).
	PromptFlag string `json:"prompt_flag,omitempty" yaml:"prompt_flag,omitempty"`

	// OutputFormatFlag is the flag to request structured output (e.g., "--output-format json").
	OutputFormatFlag string `json:"output_format_flag,omitempty" yaml:"output_format_flag,omitempty"`

	// OutputFormat is the format requested (e.g., "json", "stream-json").
	OutputFormat string `json:"output_format,omitempty" yaml:"output_format,omitempty"`

	// SessionIDEnv is the environment variable for session ID (e.g., "CLAUDE_SESSION_ID").
	SessionIDEnv string `json:"session_id_env,omitempty" yaml:"session_id_env,omitempty"`

	// ResumeFlag is the flag for resuming a session (e.g., "--resume").
	ResumeFlag string `json:"resume_flag,omitempty" yaml:"resume_flag,omitempty"`

	// SystemPromptFlag is the flag for injecting system prompt context (e.g., "--append-system-prompt").
	SystemPromptFlag string `json:"system_prompt_flag,omitempty" yaml:"system_prompt_flag,omitempty"`

	// ModelFlag is the flag for model override (e.g., "--model").
	ModelFlag string `json:"model_flag,omitempty" yaml:"model_flag,omitempty"`

	// PermissionSkipFlag is the flag for skipping permission prompts in headless mode
	// (e.g., "--dangerously-skip-permissions" for Claude, "--dangerously-bypass-approvals-and-sandbox" for Codex).
	PermissionSkipFlag string `json:"permission_skip_flag,omitempty" yaml:"permission_skip_flag,omitempty"`

	// CwdFlag is the flag for setting working directory (e.g., "-w" for some agents).
	// If empty, cwd is set via os/exec.Cmd.Dir.
	CwdFlag string `json:"cwd_flag,omitempty" yaml:"cwd_flag,omitempty"`

	// DetectCommand is the command to check if the agent is installed (defaults to Command).
	DetectCommand string `json:"detect_command,omitempty" yaml:"detect_command,omitempty"`

	// InstallHint describes how to install the agent CLI (for user guidance).
	InstallHint string `json:"install_hint,omitempty" yaml:"install_hint,omitempty"`

	// ReadyPromptPrefix is a string to look for in output that indicates the agent is ready
	// for input (e.g., "❯ " for Claude Code interactive mode).
	ReadyPromptPrefix string `json:"ready_prompt_prefix,omitempty" yaml:"ready_prompt_prefix,omitempty"`

	// ReadyDelayMs is a fallback delay (in milliseconds) before considering the agent ready,
	// used when ReadyPromptPrefix is not available.
	ReadyDelayMs int `json:"ready_delay_ms,omitempty" yaml:"ready_delay_ms,omitempty"`

	// ExtraEnv holds additional environment variables to set when spawning the agent.
	ExtraEnv map[string]string `json:"extra_env,omitempty" yaml:"extra_env,omitempty"`
}

// BuildCommand constructs a shell-injection-safe command argument list for the agent.
// This is the core of the delegation protocol: translating a task into a CLI invocation.
func (p *AgentPreset) BuildCommand(opts ExecOptions) []string {
	args := []string{p.Command}

	if opts.Prompt != "" && p.PromptFlag != "" {
		args = append(args, p.PromptFlag, opts.Prompt)
	}

	if p.OutputFormatFlag != "" && p.OutputFormat != "" {
		args = append(args, p.OutputFormatFlag, p.OutputFormat)
	}

	if opts.Model != "" && p.ModelFlag != "" {
		args = append(args, p.ModelFlag, opts.Model)
	}

	if opts.SessionID != "" && p.ResumeFlag != "" {
		args = append(args, p.ResumeFlag, opts.SessionID)
	}

	if opts.SystemPrompt != "" && p.SystemPromptFlag != "" {
		args = append(args, p.SystemPromptFlag, opts.SystemPrompt)
	}

	if opts.SkipPermissions && p.PermissionSkipFlag != "" {
		args = append(args, p.PermissionSkipFlag)
	}

	if opts.WorkDir != "" && p.CwdFlag != "" {
		args = append(args, p.CwdFlag, opts.WorkDir)
	}

	args = append(args, opts.ExtraArgs...)

	return args
}

// ExecOptions holds per-invocation parameters for building the command.
type ExecOptions struct {
	Prompt          string
	Model           string
	SessionID       string
	SystemPrompt    string
	SkipPermissions bool
	WorkDir         string
	ExtraArgs       []string
	ExtraEnv        map[string]string
}

// PresetRegistry maps agent type names to their presets.
type PresetRegistry struct {
	presets map[string]*AgentPreset
}

// NewPresetRegistry creates a registry with the built-in presets.
func NewPresetRegistry() *PresetRegistry {
	r := &PresetRegistry{presets: make(map[string]*AgentPreset)}
	for _, p := range builtinPresets() {
		r.presets[p.Name] = p
	}
	return r
}

// Get returns the preset for the given agent type.
func (r *PresetRegistry) Get(name string) (*AgentPreset, bool) {
	p, ok := r.presets[name]
	return p, ok
}

// Register adds or updates a preset.
func (r *PresetRegistry) Register(preset *AgentPreset) {
	r.presets[preset.Name] = preset
}

// List returns all registered preset names.
func (r *PresetRegistry) List() []string {
	names := make([]string, 0, len(r.presets))
	for name := range r.presets {
		names = append(names, name)
	}
	return names
}

// builtinPresets returns the default agent presets.
func builtinPresets() []*AgentPreset {
	return []*AgentPreset{
		{
			Name:               "claude",
			Command:            "claude",
			ProcessNames:       []string{"node", "claude"},
			PromptFlag:         "-p",
			OutputFormatFlag:   "--output-format",
			OutputFormat:       "json",
			SessionIDEnv:       "CLAUDE_SESSION_ID",
			ResumeFlag:         "--resume",
			SystemPromptFlag:   "--append-system-prompt",
			ModelFlag:          "--model",
			PermissionSkipFlag: "--dangerously-skip-permissions",
			DetectCommand:      "claude",
			InstallHint:        "npm install -g @anthropic-ai/claude-code",
			ReadyPromptPrefix:  "❯ ",
		},
		{
			Name:               "codex",
			Command:            "codex",
			ProcessNames:       []string{"node", "codex"},
			PromptFlag:         "-p",
			OutputFormatFlag:   "--output-format",
			OutputFormat:       "json",
			ModelFlag:          "--model",
			PermissionSkipFlag: "--dangerously-bypass-approvals-and-sandbox",
			DetectCommand:      "codex",
			InstallHint:        "npm install -g @openai/codex",
		},
		{
			Name:          "gemini",
			Command:       "gemini",
			ProcessNames:  []string{"node", "gemini"},
			PromptFlag:    "-p",
			ModelFlag:     "--model",
			DetectCommand: "gemini",
			InstallHint:   "npm install -g @anthropic-ai/gemini-cli",
			ReadyDelayMs:  3000,
		},
		{
			Name:          "aider",
			Command:       "aider",
			ProcessNames:  []string{"python", "aider"},
			PromptFlag:    "--message",
			ModelFlag:     "--model",
			DetectCommand: "aider",
			InstallHint:   "pip install aider-chat",
		},
		{
			Name:          "cursor",
			Command:       "cursor",
			ProcessNames:  []string{"cursor"},
			PromptFlag:    "-p",
			DetectCommand: "cursor",
			InstallHint:   "Download from cursor.com",
			ReadyDelayMs:  5000,
		},
		{
			Name:          "opencode",
			Command:       "opencode",
			ProcessNames:  []string{"node", "opencode"},
			PromptFlag:    "-p",
			DetectCommand: "opencode",
			InstallHint:   "npm install -g opencode-ai",
		},
	}
}
