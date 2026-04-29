package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// MCPConfig is the top-level structure of an mcp.json configuration file.
type MCPConfig struct {
	Servers map[string]ServerConfig `json:"mcpServers"`
}

// LoadConfig loads MCP server configurations from standard locations,
// merging project-level configs over user-level configs.
//
// Search order (later overrides earlier):
//  1. ~/.config/ycode/mcp.json (user)
//  2. <projectRoot>/.agents/ycode/mcp.json (project)
func LoadConfig(projectRoot string) (map[string]ServerConfig, error) {
	merged := make(map[string]ServerConfig)

	// User-level config.
	home, _ := os.UserHomeDir()
	if home != "" {
		userPath := filepath.Join(home, ".config", "ycode", "mcp.json")
		if configs, err := loadConfigFile(userPath); err == nil {
			for name, cfg := range configs {
				cfg.Name = name
				merged[name] = cfg
			}
		}
	}

	// Project-level config.
	if projectRoot != "" {
		projectPath := filepath.Join(projectRoot, ".agents", "ycode", "mcp.json")
		if configs, err := loadConfigFile(projectPath); err == nil {
			for name, cfg := range configs {
				cfg.Name = name
				merged[name] = cfg
			}
		}
	}

	return merged, nil
}

func loadConfigFile(path string) (map[string]ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return cfg.Servers, nil
}
