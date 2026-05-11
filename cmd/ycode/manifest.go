package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// writeServeManifest writes ~/.agents/ycode/manifest.json describing the live
// endpoints exposed by `ycode serve`. The manifest is the lighthouse beam:
// any foreign coding agent in the tree (Claude Code, Codex, Cursor, Continue,
// older ycode builds) can read this one file to find every ycode capability
// without poking at config or shelling out.
//
// Only fields whose underlying service is actually live are populated. Empty
// strings indicate the service did not start. The schema is versioned; bump
// schemaVersion on any breaking change.
func writeServeManifest(home string, port, natsPort int, stack *stackComponents, apiUp bool, ycodeVersion string) (string, error) {
	dir := filepath.Join(home, ".agents", "ycode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "manifest.json")

	proxy := fmt.Sprintf("http://127.0.0.1:%d", port)
	natsURL := ""
	if natsPort > 0 {
		natsURL = fmt.Sprintf("nats://127.0.0.1:%d", natsPort)
	}
	apiBase := ""
	if apiUp {
		apiBase = proxy + "/ycode/"
	}

	endpoints := map[string]string{
		"proxy":    proxy,
		"api":      apiBase,
		"otlpGRPC": stack.collectorAddr,
		"otlpHTTP": "http://127.0.0.1:4318",
		"nats":     natsURL,
	}
	if stack.gitServer != nil && stack.gitServer.Healthy() {
		endpoints["git"] = proxy + "/git/"
	}
	if stack.bonsai != nil && stack.bonsai.Healthy() {
		endpoints["graph"] = proxy + "/graph/"
	}
	if stack.memos != nil && stack.memos.Healthy() {
		endpoints["memos"] = proxy + "/memos/"
	}

	mcpHTTP := map[string]string{}
	if apiUp {
		mcpHTTP["pulse"] = proxy + "/pulse/"
	}
	if stack.gitServer != nil && stack.gitServer.Healthy() {
		mcpHTTP["gitea"] = proxy + "/gitea-mcp/"
	}
	if stack.loom != nil && stack.loom.Healthy() {
		mcpHTTP["loom"] = proxy + "/loom-mcp/"
	}

	authBlock := map[string]any{
		"scheme":           "bearer",
		"header":           "Authorization",
		"wsQueryParam":     "token",
		"tokenFile":        filepath.Join(dir, "server.token"),
		"actorHeaders":     []string{"X-Actor-User", "X-Actor-Email", "X-Actor-Roles"},
		"actorExtraPrefix": "X-Actor-Extra-",
	}
	if !apiUp {
		authBlock["enabled"] = false
	} else {
		authBlock["enabled"] = true
	}

	manifest := map[string]any{
		"schemaVersion": "3",
		"ycodeVersion":  ycodeVersion,
		"endpoints":     endpoints,
		"auth":          authBlock,
		"mcp": map[string]any{
			"stdio": map[string]any{
				"command": "ycode",
				"args":    []string{"mcp", "serve"},
			},
			"http": mcpHTTP,
		},
		"discoveryFiles": map[string]string{
			"pid":           filepath.Join(dir, "serve.pid"),
			"port":          filepath.Join(dir, "serve.port"),
			"token":         filepath.Join(dir, "server.token"),
			"collectorAddr": filepath.Join(dir, "collector.addr"),
		},
	}
	if stack.loom != nil && stack.loom.Healthy() {
		manifest["loom"] = map[string]any{
			"mcp":                        proxy + "/loom-mcp/",
			"leaseTTLDefaultSeconds":     3600,
			"leaseTTLMaxSeconds":         28800,
			"subAgentIdentityConvention": "loom:<label>",
			"cloneURLTemplate":           proxy + "/git/admin/{slug}.git",
			"tokenFile":                  filepath.Join(home, ".agents", "ycode", "gitea", "admin.token"),
			"sandboxRoot":                filepath.Join(home, ".agents", "ycode", "gitea", "loom", "sandboxes"),
			"branchNamePattern":          "agent/agent-loom:<label>-<id8>/free-<rand>",
		}
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
