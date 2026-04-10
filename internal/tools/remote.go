package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	remoteTriggerTimeout = 30 * time.Second
	maxBodySize          = 8 * 1024 // 8KB
)

// RegisterRemoteHandler registers the RemoteTrigger tool handler.
func RegisterRemoteHandler(r *Registry) {
	spec, ok := r.Get("RemoteTrigger")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			URL    string `json:"url"`
			Method string `json:"method,omitempty"`
			Body   string `json:"body,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse RemoteTrigger input: %w", err)
		}

		method := params.Method
		if method == "" {
			method = http.MethodPost
		}

		client := &http.Client{Timeout: remoteTriggerTimeout}

		var bodyReader io.Reader
		if params.Body != "" {
			if len(params.Body) > maxBodySize {
				params.Body = params.Body[:maxBodySize]
			}
			bodyReader = strings.NewReader(params.Body)
		}

		req, err := http.NewRequestWithContext(ctx, method, params.URL, bodyReader)
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("trigger %s: %w", params.URL, err)
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
		if err != nil {
			return "", fmt.Errorf("read response: %w", err)
		}

		return fmt.Sprintf("Status: %d\n%s", resp.StatusCode, string(respBody)), nil
	}
}
