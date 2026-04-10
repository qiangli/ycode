package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/config"
)

// RegisterConfigHandler registers the Config tool handler.
func RegisterConfigHandler(r *Registry, cfg *config.Config) {
	spec, ok := r.Get("Config")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Action string `json:"action"`
			Key    string `json:"key"`
			Value  any    `json:"value,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse Config input: %w", err)
		}

		switch params.Action {
		case "get":
			val, ok := cfg.Get(params.Key)
			if !ok {
				return fmt.Sprintf("config key %q not found", params.Key), nil
			}
			data, _ := json.Marshal(val)
			return string(data), nil
		case "set":
			cfg.Set(params.Key, params.Value)
			return fmt.Sprintf("set %s = %v", params.Key, params.Value), nil
		default:
			return "", fmt.Errorf("unknown config action: %s", params.Action)
		}
	}
}
