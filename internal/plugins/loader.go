package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/tools"
)

// Loader registers discovered plugins into a tool registry.
type Loader struct {
	registry *tools.Registry
	logger   *slog.Logger
}

// NewLoader creates a plugin loader targeting the given registry.
func NewLoader(registry *tools.Registry, logger *slog.Logger) *Loader {
	if logger == nil {
		logger = slog.Default()
	}
	return &Loader{
		registry: registry,
		logger:   logger,
	}
}

// LoadAll discovers and registers plugins from the given directories.
// Returns the number of tools registered and any non-fatal warnings.
func (l *Loader) LoadAll(dirs ...string) (int, []string) {
	discovered, err := DiscoverManifests(dirs...)
	if err != nil {
		return 0, []string{fmt.Sprintf("discovery error: %v", err)}
	}

	var count int
	var warnings []string

	for _, dp := range discovered {
		n, warns := l.loadPlugin(dp)
		count += n
		warnings = append(warnings, warns...)
	}

	return count, warnings
}

// loadPlugin registers all tools from a single plugin.
func (l *Loader) loadPlugin(dp DiscoveredPlugin) (int, []string) {
	var count int
	var warnings []string

	for _, td := range dp.Manifest.Tools {
		handler := l.makeCommandHandler(dp.Dir, td.Command)

		spec := &tools.ToolSpec{
			Name:            fmt.Sprintf("%s.%s", dp.Manifest.Name, td.Name),
			Description:     td.Description,
			InputSchema:     td.InputSchema,
			Source:          tools.SourcePlugin,
			Handler:         handler,
			AlwaysAvailable: td.AlwaysAvailable,
		}

		if err := l.registry.Register(spec); err != nil {
			warnings = append(warnings, fmt.Sprintf("plugin %s: tool %s: %v", dp.Manifest.Name, td.Name, err))
			continue
		}

		l.logger.Debug("registered plugin tool",
			"plugin", dp.Manifest.Name,
			"tool", spec.Name,
		)
		count++
	}

	return count, warnings
}

// makeCommandHandler creates a ToolFunc that executes a shell command.
// The tool input JSON is passed via stdin.
func (l *Loader) makeCommandHandler(pluginDir string, command string) tools.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		timeout := 30 * time.Second
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir = pluginDir

		if len(input) > 0 {
			cmd.Stdin = strings.NewReader(string(input))
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return string(output), fmt.Errorf("plugin command failed: %w\noutput: %s", err, output)
		}

		return strings.TrimSpace(string(output)), nil
	}
}
