package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/bash"
)

// RegisterBashHandler registers the bash tool handler.
func RegisterBashHandler(r *Registry, workDir string) {
	spec, ok := r.Get("bash")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params bash.ExecParams
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse bash input: %w", err)
		}
		if params.WorkDir == "" {
			params.WorkDir = workDir
		}

		result, err := bash.Execute(ctx, params)
		if err != nil {
			return "", err
		}

		output := result.Stdout
		if result.Stderr != "" {
			if output != "" {
				output += "\n"
			}
			output += result.Stderr
		}
		if result.ExitCode != 0 {
			output += fmt.Sprintf("\n(exit code: %d)", result.ExitCode)
		}
		if output == "" {
			output = "(no output)"
		}
		return output, nil
	}
}
