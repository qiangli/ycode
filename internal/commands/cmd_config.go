package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// configHandler returns a handler that displays configuration files and merged settings.
func configHandler(deps *RuntimeDeps) HandlerFunc {
	return func(ctx context.Context, args string) (string, error) {
		section := strings.TrimSpace(args)
		cfg := deps.Config

		var b strings.Builder

		// Discover config files and their status.
		type configFile struct {
			path   string
			source string
			loaded bool
		}

		var discovered []configFile
		paths := []struct {
			dir    string
			source string
		}{
			{deps.ConfigDirs.UserDir, "user"},
			{deps.ConfigDirs.ProjectDir, "project"},
			{deps.ConfigDirs.LocalDir, "local"},
		}

		loadedCount := 0
		for _, p := range paths {
			if p.dir == "" {
				continue
			}
			path := filepath.Join(p.dir, "settings.json")
			_, err := os.Stat(path)
			loaded := err == nil
			if loaded {
				loadedCount++
			}
			discovered = append(discovered, configFile{
				path:   path,
				source: p.source,
				loaded: loaded,
			})
		}

		fmt.Fprintf(&b, "Config\n")
		fmt.Fprintf(&b, "  Working directory  %s\n", deps.WorkDir)
		fmt.Fprintf(&b, "  Loaded files       %d\n", loadedCount)

		b.WriteString("\nDiscovered files\n")
		for _, f := range discovered {
			status := "loaded"
			if !f.loaded {
				status = "missing"
			}
			fmt.Fprintf(&b, "  [%s] %s  (%s)\n", f.source, f.path, status)
		}

		if cfg == nil {
			return b.String(), nil
		}

		// Show section or all merged settings.
		switch section {
		case "":
			b.WriteString("\nMerged settings\n")
			fmt.Fprintf(&b, "  model              %s\n", cfg.Model)
			if deps.ProviderKind != nil {
				if pk := deps.ProviderKind(); pk != "" {
					fmt.Fprintf(&b, "  provider           %s\n", pk)
				}
			}
			fmt.Fprintf(&b, "  maxTokens          %d\n", cfg.MaxTokens)
			if cfg.Temperature != nil {
				fmt.Fprintf(&b, "  temperature        %.2f\n", *cfg.Temperature)
			}
			fmt.Fprintf(&b, "  permissionMode     %s\n", cfg.PermissionMode)
			fmt.Fprintf(&b, "  autoCompact        %v\n", cfg.AutoCompactEnabled)
			fmt.Fprintf(&b, "  autoMemory         %v\n", cfg.AutoMemoryEnabled)
			if cfg.SessionDir != "" {
				fmt.Fprintf(&b, "  sessionDir         %s\n", cfg.SessionDir)
			}
			if len(cfg.Aliases) > 0 {
				b.WriteString("  aliases:\n")
				for k, v := range cfg.Aliases {
					fmt.Fprintf(&b, "    %s = %s\n", k, v)
				}
			}
			if len(cfg.Custom) > 0 {
				b.WriteString("  custom:\n")
				for k, v := range cfg.Custom {
					fmt.Fprintf(&b, "    %s = %v\n", k, v)
				}
			}

		case "model":
			fmt.Fprintf(&b, "\nModel\n")
			fmt.Fprintf(&b, "  model              %s\n", cfg.Model)
			if deps.ProviderKind != nil {
				if pk := deps.ProviderKind(); pk != "" {
					fmt.Fprintf(&b, "  provider           %s\n", pk)
				}
			}
			if len(cfg.Aliases) > 0 {
				b.WriteString("  aliases:\n")
				for k, v := range cfg.Aliases {
					fmt.Fprintf(&b, "    %s = %s\n", k, v)
				}
			}

		case "permissions":
			fmt.Fprintf(&b, "\nPermission mode: %s\n", cfg.PermissionMode)

		case "memory":
			fmt.Fprintf(&b, "\nMemory settings\n")
			fmt.Fprintf(&b, "  autoMemory   %v\n", cfg.AutoMemoryEnabled)
			fmt.Fprintf(&b, "  autoDream    %v\n", cfg.AutoDreamEnabled)

		case "session":
			fmt.Fprintf(&b, "\nSession settings\n")
			fmt.Fprintf(&b, "  autoCompact          %v\n", cfg.AutoCompactEnabled)
			fmt.Fprintf(&b, "  fileCheckpointing    %v\n", cfg.FileCheckpointingEnabled)
			if cfg.SessionDir != "" {
				fmt.Fprintf(&b, "  sessionDir           %s\n", cfg.SessionDir)
			}

		default:
			fmt.Fprintf(&b, "\nUnknown section %q. Available: model, permissions, memory, session\n", section)
		}

		return b.String(), nil
	}
}
