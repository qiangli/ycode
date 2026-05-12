package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// buildServeManifest assembles the manifest map describing the live endpoints
// of `ycode serve`. The result is the canonical "full" manifest — including
// local-filesystem paths (token files, sandbox roots) intended for callers
// running on the same host as the server.
//
// The schema is versioned; bump schemaVersion on any breaking change.
func buildServeManifest(home string, port, natsPort int, stack *stackComponents, apiUp bool, ycodeVersion string) map[string]any {
	dir := filepath.Join(home, ".agents", "ycode")

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

	// Composite endpoint — the single URL every foreign agent points at
	// (Agent OS syscall interface). Always live: treesitter and skills
	// families are always-on. Per-family routes (/gitea-mcp/, /loom-mcp/,
	// /pulse/) were retired in schemaVersion 4; clients using those URLs
	// must switch to /mcp/.
	mcpHTTP := map[string]string{
		"ycode": proxy + "/mcp/",
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
		"schemaVersion": "4",
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
			"mcp":                        proxy + "/mcp/",
			"leaseTTLDefaultSeconds":     3600,
			"leaseTTLMaxSeconds":         28800,
			"subAgentIdentityConvention": "loom:<label>",
			"cloneURLTemplate":           proxy + "/git/admin/{slug}.git",
			"tokenFile":                  filepath.Join(home, ".agents", "ycode", "gitea", "admin.token"),
			"sandboxRoot":                filepath.Join(home, ".agents", "ycode", "gitea", "loom", "sandboxes"),
			"branchNamePattern":          "agent/agent-loom:<label>-<id8>/free-<rand>",
		}
	}

	// Canvas block — agent-rendered generative UI service. Foreign agents
	// discover the A2UI op format ycode speaks, the bus event types that
	// carry it, the MCP tools that publish, the canvas route to subscribe
	// to via WS, and the well-known default session. The block is only
	// advertised when the API stack is up (canvas requires a bus to
	// publish onto). See internal/runtime/widget and docs/strategy.md.
	if apiUp {
		canvas := map[string]any{
			"a2uiVersion":    "v0.9",
			"a2uiCatalog":    "https://a2ui.org/specification/v0_9/basic_catalog.json",
			"events":         []string{"state.update", "state.mutate"},
			"defaultSession": "canvas-default",
			"route":          proxy + "/ycode/canvas/",
			"wsTemplate":     proxy + "/ycode/api/sessions/{sessionId}/ws",
			"tools": map[string]string{
				"renderA2UI":   "agent_render_a2ui",
				"renderWidget": "agent_render_widget",
			},
			// First-class A2UI surfaces ycode publishes will be listed here
			// as they ship (e.g. "health", "memos", "kanban", "lanes").
			// Empty for now — surfaces land in their respective tracks.
			"firstClassSurfaces": []string{},
		}
		manifest["canvas"] = canvas
	}
	return manifest
}

// publicServeManifest returns the subset of the full manifest safe to expose
// over HTTP without authentication. It strips every field that names a local
// filesystem path, since those are useless (and slightly leaky) to a remote
// caller. Remote clients learn from this what URLs to talk to and how to
// authenticate — they then obtain a token out-of-band (via `ycode pair` or
// operator paste) and call /manifest for the authenticated full view.
func publicServeManifest(full map[string]any) map[string]any {
	out := map[string]any{
		"schemaVersion": full["schemaVersion"],
		"ycodeVersion":  full["ycodeVersion"],
		"endpoints":     copyStringMapOmitting(full["endpoints"], nil),
		"mcp":           publicMCPBlock(full["mcp"]),
	}
	if auth, ok := full["auth"].(map[string]any); ok {
		out["auth"] = map[string]any{
			"scheme":  auth["scheme"],
			"header":  auth["header"],
			"enabled": auth["enabled"],
		}
	}
	if loom, ok := full["loom"].(map[string]any); ok {
		out["loom"] = map[string]any{
			"mcp":                        loom["mcp"],
			"leaseTTLDefaultSeconds":     loom["leaseTTLDefaultSeconds"],
			"leaseTTLMaxSeconds":         loom["leaseTTLMaxSeconds"],
			"subAgentIdentityConvention": loom["subAgentIdentityConvention"],
			"cloneURLTemplate":           loom["cloneURLTemplate"],
			"branchNamePattern":          loom["branchNamePattern"],
		}
	}
	if canvas, ok := full["canvas"].(map[string]any); ok {
		// Canvas block has no local-path fields — entire block is safe to
		// re-publish unauthenticated. Foreign agents need the route,
		// wsTemplate, A2UI version, and tool names to interop.
		out["canvas"] = canvas
	}
	return out
}

// publicMCPBlock returns the http URLs only (stdio command + discoveryFiles
// are local-only and stripped).
func publicMCPBlock(in any) map[string]any {
	m, _ := in.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if h, ok := m["http"]; ok {
		out["http"] = h
	}
	return out
}

// copyStringMapOmitting clones a map[string]string-like value, dropping any
// entry whose value looks like a local filesystem path. omit may be used to
// drop specific keys explicitly.
func copyStringMapOmitting(in any, omit map[string]struct{}) map[string]string {
	out := map[string]string{}
	m, _ := in.(map[string]string)
	if m == nil {
		return out
	}
	for k, v := range m {
		if _, drop := omit[k]; drop {
			continue
		}
		if v == "" {
			continue
		}
		// Defensive: never publish a path that doesn't look like a URL.
		if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "nats://") {
			continue
		}
		out[k] = v
	}
	return out
}

// writeServeManifest writes ~/.agents/ycode/manifest.json — the lighthouse
// beam for foreign coding agents on the same host. The HTTP-served variant
// (/manifest, /.well-known/ycode-manifest.json) is the remote-safe analog;
// both originate from buildServeManifest.
func writeServeManifest(home string, port, natsPort int, stack *stackComponents, apiUp bool, ycodeVersion string) (string, error) {
	dir := filepath.Join(home, ".agents", "ycode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "manifest.json")
	manifest := buildServeManifest(home, port, natsPort, stack, apiUp, ycodeVersion)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// manifestPublicHandler serves the public subset of the manifest at
// /.well-known/ycode-manifest.json. Unauthenticated — any remote caller
// uses this to discover what URLs to talk to. Secrets and local paths
// are never included.
func manifestPublicHandler(full map[string]any) http.Handler {
	pub := publicServeManifest(full)
	body, _ := json.MarshalIndent(pub, "", "  ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	})
}

// manifestFullHandler serves the full manifest at /manifest, gated by a
// bearer token equal to ~/.agents/ycode/server.token. When authDisabled is
// true (e.g. dev mode), the gate is open. The full manifest includes local
// filesystem paths and is only useful (and only safe) to callers that already
// possess the bearer token.
func manifestFullHandler(full map[string]any, tokenFile string, authDisabled bool) http.Handler {
	body, _ := json.MarshalIndent(full, "", "  ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authDisabled {
			if !authorizedBearer(r, tokenFile) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="ycode"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	})
}

// authorizedBearer compares r's Authorization: Bearer <token> header against
// the contents of tokenFile. Returns true on exact match. Missing or empty
// token files yield false (fail closed).
func authorizedBearer(r *http.Request, tokenFile string) bool {
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	if got == "" {
		return false
	}
	want, err := os.ReadFile(tokenFile)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(want)) == got
}
