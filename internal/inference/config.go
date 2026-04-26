// Package inference provides the embedded Ollama inference engine component.
//
// It embeds Ollama's pure-Go packages (API types, model management, manifest
// handling) and manages the C++ inference runner as an external subprocess.
// The runner is discovered, spawned, health-checked, and auto-restarted
// with exponential backoff.
package inference

import "github.com/qiangli/ycode/internal/runtime/config"

// Config is an alias for the centralized inference configuration.
type Config = config.InferenceConfig

// HFConfig holds Hugging Face Hub configuration.
type HFConfig struct {
	// Token is the Hugging Face API token for gated models.
	// Falls back to $HF_TOKEN environment variable.
	Token string `json:"token,omitempty"`

	// CacheDir is the directory for downloaded HF models.
	// Defaults to ~/.cache/huggingface/hub.
	CacheDir string `json:"cacheDir,omitempty"`

	// PreferGGUF is the preferred GGUF quantization (e.g. "Q4_K_M").
	PreferGGUF string `json:"preferGGUF,omitempty"`
}
