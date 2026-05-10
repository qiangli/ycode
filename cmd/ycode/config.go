package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// newConfigCmd builds `ycode config ...` for reading and writing
// ~/.config/ycode/settings.json without hand-editing JSON.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and write ~/.config/ycode/settings.json",
		Long: `Manage ycode's user-global settings.json without hand-editing.

Examples:
  ycode config show                          # print the whole file
  ycode config path                          # print the file path
  ycode config get model                     # print one field
  ycode config set model claude-sonnet-4-6   # set one field
  ycode config set inference.enabled true    # set a nested field
  ycode config unset model                   # remove a field`,
	}
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigPathCmd())
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigUnsetCmd())
	return cmd
}

// userConfigPath returns ~/.config/ycode/settings.json.
func userConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ycode", "settings.json"), nil
}

// loadConfig reads settings.json into a generic map. Missing file
// returns an empty map (so set/unset work in a fresh install).
func loadConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// saveConfig atomically writes the map back as pretty JSON.
func saveConfig(path string, m map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the full settings.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := userConfigPath()
			if err != nil {
				return err
			}
			m, err := loadConfig(path)
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path to settings.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := userConfigPath()
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print one field (dot-separated for nested keys)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := userConfigPath()
			if err != nil {
				return err
			}
			m, err := loadConfig(path)
			if err != nil {
				return err
			}
			v, ok := getDotted(m, args[0])
			if !ok {
				return fmt.Errorf("key not set: %s", args[0])
			}
			switch x := v.(type) {
			case string:
				fmt.Println(x)
			default:
				data, _ := json.Marshal(v)
				fmt.Println(string(data))
			}
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set one field (dot-separated for nested keys; values auto-typed)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := userConfigPath()
			if err != nil {
				return err
			}
			m, err := loadConfig(path)
			if err != nil {
				return err
			}
			setDotted(m, args[0], parseValue(args[1]))
			if err := saveConfig(path, m); err != nil {
				return err
			}
			fmt.Printf("set %s = %s in %s\n", args[0], args[1], path)
			return nil
		},
	}
}

func newConfigUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove one field (dot-separated for nested keys)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := userConfigPath()
			if err != nil {
				return err
			}
			m, err := loadConfig(path)
			if err != nil {
				return err
			}
			if !unsetDotted(m, args[0]) {
				return fmt.Errorf("key not set: %s", args[0])
			}
			if err := saveConfig(path, m); err != nil {
				return err
			}
			fmt.Printf("unset %s in %s\n", args[0], path)
			return nil
		},
	}
}

// parseValue auto-types the string into bool/int/float/JSON if it
// looks like one; otherwise returns the string as-is.
func parseValue(s string) any {
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	// Try JSON (objects, arrays); accept on success, else treat as string.
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			return v
		}
	}
	return s
}

// getDotted walks "a.b.c" through nested maps.
func getDotted(m map[string]any, key string) (any, bool) {
	parts := strings.Split(key, ".")
	var cur any = m
	for _, p := range parts {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = mm[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// setDotted sets m[a][b][c] = value, creating intermediate maps.
func setDotted(m map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	cur := m
	for i, p := range parts {
		if i == len(parts)-1 {
			cur[p] = value
			return
		}
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
}

// unsetDotted removes m[a][b][c]. Returns true if it existed.
func unsetDotted(m map[string]any, key string) bool {
	parts := strings.Split(key, ".")
	cur := m
	for i, p := range parts {
		if i == len(parts)-1 {
			if _, ok := cur[p]; !ok {
				return false
			}
			delete(cur, p)
			return true
		}
		next, ok := cur[p].(map[string]any)
		if !ok {
			return false
		}
		cur = next
	}
	return false
}
