package benchmark

import (
	_ "embed"
	"path/filepath"
	"time"
)

//go:embed dockerfiles/Dockerfile.ycode
var dockerfileYcode []byte

//go:embed dockerfiles/Dockerfile.node
var dockerfileNode []byte

//go:embed dockerfiles/Dockerfile.rust
var dockerfileRust []byte

//go:embed dockerfiles/Dockerfile.python
var dockerfilePython []byte

// ToolDriver describes how to invoke a tool's /init in a container.
type ToolDriver struct {
	Name        string            // tool name for display
	Dockerfile  []byte            // embedded Dockerfile content
	SourceDir   string            // absolute path to tool source (build context)
	BuildArgs   map[string]string // docker build args
	Env         map[string]string // container runtime env vars
	InitCommand string            // shell command to exec inside container
	OutputFiles []string          // files to check for generated output
	Timeout     time.Duration     // per-tool timeout
	ImageName   string            // container image name
	Skip        bool              // skip this tool
	SkipReason  string            // reason for skipping
}

// DefaultDrivers returns configs for all tools to benchmark.
// projectRoot is the ycode repo root (for resolving priorart/ paths).
// ollamaURL is the Ollama HTTP endpoint (e.g., "http://host.containers.internal:11434").
// model is the Ollama model name (e.g., "qwen2.5-coder:14b").
func DefaultDrivers(projectRoot, ollamaURL, model string) []ToolDriver {
	commonEnv := map[string]string{
		"OPENAI_API_KEY":  "ollama",
		"OPENAI_BASE_URL": ollamaURL + "/v1",
		"OLLAMA_HOST":     ollamaURL,
		"MODEL":           model,
		// Suppress interactive prompts.
		"TERM": "dumb",
		"CI":   "true",
	}

	return []ToolDriver{
		{
			Name:        "ycode",
			Dockerfile:  dockerfileYcode,
			SourceDir:   projectRoot,
			Env:         commonEnv,
			InitCommand: "ycode /init 2>/dev/null || true",
			OutputFiles: []string{"AGENTS.md"},
			Timeout:     3 * time.Minute,
			ImageName:   "bench-ycode",
		},
		{
			Name:        "opencode",
			Dockerfile:  dockerfileNode,
			SourceDir:   filepath.Join(projectRoot, "priorart/opencode"),
			Env:         commonEnv,
			InitCommand: "echo '/init' | timeout 120 opencode 2>/dev/null || true",
			OutputFiles: []string{"AGENTS.md", "CLAUDE.md"},
			Timeout:     3 * time.Minute,
			ImageName:   "bench-opencode",
		},
		{
			Name:        "clawcode",
			Dockerfile:  dockerfileRust,
			SourceDir:   filepath.Join(projectRoot, "priorart/clawcode"),
			BuildArgs:   map[string]string{"BINARY_NAME": "clawcode", "CARGO_DIR": "rust"},
			Env:         commonEnv,
			InitCommand: "echo '/init' | timeout 120 tool 2>/dev/null || true",
			OutputFiles: []string{"CLAUDE.md", "AGENTS.md"},
			Timeout:     3 * time.Minute,
			ImageName:   "bench-clawcode",
		},
		{
			Name:        "codex",
			Dockerfile:  dockerfileRust,
			SourceDir:   filepath.Join(projectRoot, "priorart/codex"),
			BuildArgs:   map[string]string{"BINARY_NAME": "codex", "CARGO_DIR": "codex-rs"},
			Env:         commonEnv,
			InitCommand: `tool --message "Analyze this repository and generate an AGENTS.md file. Include: project structure, build commands, test commands, and coding conventions. Write the file to AGENTS.md." --approval-mode full-auto 2>/dev/null || true`,
			OutputFiles: []string{"AGENTS.md", "CLAUDE.md"},
			Timeout:     3 * time.Minute,
			ImageName:   "bench-codex",
		},
		{
			Name:        "aider",
			Dockerfile:  dockerfilePython,
			SourceDir:   filepath.Join(projectRoot, "priorart/aider"),
			Env:         commonEnv,
			InitCommand: `aider --yes --no-git --message "Generate an AGENTS.md file for this repository. Include: project structure, build commands, test commands, and coding conventions. Write the file to AGENTS.md." --openai-api-base "$OLLAMA_HOST/v1" --model "openai/$MODEL" 2>/dev/null || true`,
			OutputFiles: []string{"AGENTS.md", "CLAUDE.md"},
			Timeout:     3 * time.Minute,
			ImageName:   "bench-aider",
		},
		{
			Name:       "gemini-cli",
			Skip:       true,
			SkipReason: "requires Google Gemini API (no OpenAI-compat mode)",
			ImageName:  "bench-gemini-cli",
		},
	}
}
