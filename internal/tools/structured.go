package tools

import (
	"context"
	"encoding/json"
)

// RegisterStructuredOutputHandler registers the StructuredOutput tool.
func RegisterStructuredOutputHandler(r *Registry) {
	spec, ok := r.Get("StructuredOutput")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		// StructuredOutput just passes through the JSON input as the result.
		return string(input), nil
	}
}
