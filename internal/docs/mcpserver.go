package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// MCPHandler exposes the agent-facing capability catalog as MCP tools
// and resources. Mirror of the cobra surface in cmd/ycode/docs.go —
// both delegate to the same package-level functions (IndexBody, Get,
// Topics) so there is no chance of content drift between transports.
//
// ============================================================================
// SAFEGUARDS — read before editing this file
// ============================================================================
//
//  1. THIS HANDLER IS READ-ONLY. Every tool returns embedded markdown.
//     Do not add a write-capable tool here (no save_doc, no update_doc,
//     no fetch_external). The default PermissionAware ceiling is
//     ModeReadOnly so a write tool would be DENIED by StaticGate
//     anyway, but a future contributor might add ModeWorkspaceWrite to
//     "fix" the denial. Don't. Curated docs live in source control and
//     ship with the binary; runtime mutation defeats the whole point.
//
//  2. CONTENT COMES FROM ONE PLACE ONLY: the agent/ embedded FS via
//     IndexBody / Registry / Get. Do not parallel-implement parsing or
//     formatting here. If the shape needs to change for MCP callers
//     (e.g., a topic shouldn't include its raw frontmatter), add a
//     formatting function in this package and have BOTH the cobra
//     command and this handler call it.
//
//  3. TOOL NAMES ARE PART OF THE PUBLIC API. `list_docs` and `get_doc`
//     are namespaced as `mcp__ycode__list_docs` / `mcp__ycode__get_doc`
//     by clients (Claude Code, Codex, etc.). Renaming them silently
//     breaks every system prompt that references them. If you must
//     rename, add the new name AND keep the old as a deprecated alias
//     for one release.
//
//  4. RESOURCE URI SCHEME IS LOAD-BEARING. `ycode://docs/<slug>` and
//     `ycode://docs/_index` are how MCP resource-capable clients
//     discover and read docs without a tool call. The scheme matches
//     `ycode://skills/<name>` deliberately so foreign agents see a
//     consistent ycode URI family.
//
// ============================================================================

// MCPHandler implements mcp.ServerHandler.
type MCPHandler struct{}

// NewMCPHandler returns a handler ready to mount in
// mcp.NewCompositeHandler(...). Stateless — no constructor args, no
// goroutines, no I/O at construction time.
func NewMCPHandler() *MCPHandler { return &MCPHandler{} }

// resourceURI is the canonical scheme prefix for doc resources.
// Matches `ycode://skills/` deliberately — see safeguard #4.
const resourceURI = "ycode://docs/"

// indexResourceSlug is the suffix appended to resourceURI for the
// hand-curated index. Kept distinct from registered topic slugs by the
// leading underscore (slugPattern in parse.go rejects underscores).
const indexResourceSlug = "_index"

// ListTools exposes two tools:
//
//   - list_docs — return the topic index (JSON: slug, summary, when, max_lines).
//   - get_doc   — return the full curated body for one topic, or the
//     index when topic is empty / "_index".
func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "list_docs",
			Description: "List ycode's agent-facing capability prompts. Returns an array of " +
				"{topic, summary, when, max_lines} entries — call this first when you " +
				"want to discover what ycode can do. Follow up with get_doc to fetch " +
				"the prompt body for a specific topic. The same content is reachable " +
				"via the `ycode docs` shell command.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name: "get_doc",
			Description: "Return the agent-facing prompt for one ycode capability. With no " +
				"topic (or topic=\"_index\"), returns the curated topic index. Topics " +
				"are short, action-oriented markdown documents ending in an `## Exact " +
				"calls` section listing copy-pasteable invocations.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"topic": {"type": "string", "description": "Topic slug, e.g. \"mcp\". Empty or \"_index\" returns the index."}
				}
			}`),
		},
	}
}

// ListResources exposes one resource per topic plus the index. Foreign
// agents that prefer the MCP resources abstraction over tool calls can
// resources/list and resources/read the same content.
func (h *MCPHandler) ListResources() []mcp.Resource {
	topics, err := Topics()
	if err != nil {
		// Lint test would have caught this at build time; if we somehow
		// reach this branch in production, surface the index alone so
		// at least bootstrap still works.
		return []mcp.Resource{{
			URI:         resourceURI + indexResourceSlug,
			Name:        "ycode docs index",
			Description: "Topic index for ycode's agent-facing capability prompts.",
			MimeType:    "text/markdown",
		}}
	}

	out := make([]mcp.Resource, 0, len(topics)+1)
	out = append(out, mcp.Resource{
		URI:         resourceURI + indexResourceSlug,
		Name:        "ycode docs index",
		Description: "Topic index for ycode's agent-facing capability prompts. Start here.",
		MimeType:    "text/markdown",
	})
	for _, slug := range topics {
		d, err := Get(slug)
		if err != nil {
			continue
		}
		out = append(out, mcp.Resource{
			URI:         resourceURI + slug,
			Name:        "ycode docs: " + slug,
			Description: d.Summary + ". When to use: " + d.When,
			MimeType:    "text/markdown",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out
}

// HandleToolCall dispatches list_docs and get_doc.
func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "list_docs":
		return formatTopicList()

	case "get_doc":
		var args struct {
			Topic string `json:"topic"`
		}
		// Empty input is allowed — defaults to returning the index.
		if len(input) > 0 && !isEmptyJSON(input) {
			if err := json.Unmarshal(input, &args); err != nil {
				return "", fmt.Errorf("get_doc: parse args: %w", err)
			}
		}
		return resolveDoc(args.Topic)

	default:
		return "", fmt.Errorf("unknown tool: %q", name)
	}
}

// ReadResource serves ycode://docs/<slug> and ycode://docs/_index.
func (h *MCPHandler) ReadResource(ctx context.Context, uri string) (string, error) {
	if !strings.HasPrefix(uri, resourceURI) {
		return "", fmt.Errorf("unknown resource URI: %q", uri)
	}
	slug := strings.TrimPrefix(uri, resourceURI)
	return resolveDoc(slug)
}

// RequiredMode declares the permission ceiling for every tool exposed
// by this handler. Both tools are pure reads of embedded content —
// ModeReadOnly is the truthful (and safest) declaration. Implementing
// PermissionAware here is what lets the standalone `ycode mcp serve`
// stdio default (StaticGate{Ceiling: ModeReadOnly}) keep these tools
// callable while denying any future write-capable handler.
func (h *MCPHandler) RequiredMode(toolName string) mcp.PermissionMode {
	switch toolName {
	case "list_docs", "get_doc":
		return mcp.ModeReadOnly
	default:
		return mcp.ModeReadOnly
	}
}

// resolveDoc returns the body for a topic slug. Empty string or the
// reserved "_index" slug resolves to the curated index. Unknown slugs
// return an error formatted with the available topics, matching the
// stderr message from the cobra command for consistency.
func resolveDoc(slug string) (string, error) {
	if slug == "" || slug == indexResourceSlug {
		return IndexBody()
	}
	d, err := Get(slug)
	if err != nil {
		topics, _ := Topics()
		return "", fmt.Errorf("unknown topic %q; available: %s",
			slug, strings.Join(topics, ", "))
	}
	return d.Raw, nil
}

// formatTopicList returns the same JSON shape as `ycode docs --list`.
// Single source of truth lives here; the cobra command should call this
// helper if/when its output is normalized (today it inlines an
// equivalent struct — acceptable while there's only one consumer per
// transport, but consolidate when a third caller appears).
func formatTopicList() (string, error) {
	topics, err := Topics()
	if err != nil {
		return "", err
	}
	type row struct {
		Topic    string `json:"topic"`
		Summary  string `json:"summary"`
		When     string `json:"when"`
		MaxLines int    `json:"max_lines"`
	}
	out := make([]row, 0, len(topics))
	for _, slug := range topics {
		d, err := Get(slug)
		if err != nil {
			return "", err
		}
		out = append(out, row{Topic: d.Topic, Summary: d.Summary, When: d.When, MaxLines: d.MaxLines})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// isEmptyJSON returns true for nil / whitespace / literal `null` /
// empty-object input. Used by HandleToolCall to tolerate the variety
// of empty-arg shapes different MCP clients send.
func isEmptyJSON(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == "{}"
}
