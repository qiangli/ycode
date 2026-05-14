package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// MCPHandler exposes ycode's code-knowledge-graph (gfy) to external
// coding agents over MCP. All tools are read-only — they query the
// cached graph at .agents/ycode/graph.json (or rebuild on demand) and
// return text summaries.
//
// Builds are expensive (full repo scan, symbol extraction, community
// detection); we always try Load() first and only rebuild when the
// cache is missing or the caller explicitly asks via `force_rebuild`.
type MCPHandler struct{}

// NewMCPHandler constructs the handler. Stateless — each call resolves
// the cache path from the provided cwd (or the server's cwd) and reads
// or builds as needed.
func NewMCPHandler() *MCPHandler { return &MCPHandler{} }

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "graph_summary",
			Description: "Return a high-level architectural summary of the project's code-knowledge graph: " +
				"node/edge counts, language mix, community boundaries, the most-connected 'god nodes', and " +
				"surprising cross-module connections. Cheap if a cache exists at .agents/ycode/graph.json. " +
				"Pass `force_rebuild=true` to scan the repo from scratch (slow on large projects).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":           {"type": "string", "description": "Project root. Defaults to the server's cwd."},
					"force_rebuild": {"type": "boolean", "description": "If true, ignore the cached graph and rescan the repo."}
				}
			}`),
		},
		{
			Name: "graph_query",
			Description: "Semantic search across graph nodes followed by a BFS expansion. Returns the matched " +
				"subgraph as text (nodes + edges). Use to answer questions like 'what touches the auth flow?' or " +
				"'show me everything that calls into the cache layer.'",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":      {"type": "string"},
					"question": {"type": "string", "description": "Natural-language query; matched against node labels."},
					"depth":    {"type": "integer", "description": "BFS depth from each seed match. Default 2."}
				},
				"required": ["question"]
			}`),
		},
		{
			Name:        "graph_node",
			Description: "Look up a single node by label or ID and return its attributes plus degree.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":   {"type": "string"},
					"label": {"type": "string", "description": "Node label or ID (function name, type name, etc.)."}
				},
				"required": ["label"]
			}`),
		},
		{
			Name:        "graph_neighbors",
			Description: "List direct neighbors of a node, optionally filtered by relation type (calls, imports, contains, ...).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":             {"type": "string"},
					"label":           {"type": "string"},
					"relation_filter": {"type": "string", "description": "Optional. Restrict to edges with this relation."}
				},
				"required": ["label"]
			}`),
		},
		{
			Name:        "graph_god_nodes",
			Description: "List the most-connected entities (architectural linchpins). Changes here have the widest blast radius.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":   {"type": "string"},
					"top_n": {"type": "integer", "description": "Number of god nodes to return. Default 10."}
				}
			}`),
		},
		{
			Name:        "graph_shortest_path",
			Description: "Find the shortest path between two nodes in the graph (by label or ID). Returns the chain of labels.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":      {"type": "string"},
					"source":   {"type": "string"},
					"target":   {"type": "string"},
					"max_hops": {"type": "integer", "description": "Hop budget. Default 8."}
				},
				"required": ["source", "target"]
			}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

// RequiredMode: all graph tools are reads. Build/rebuild walks the
// filesystem under cwd but writes only to the cache file under
// .agents/ycode/graph.json — that is project-managed scratch, not a
// user-write of source code. Classify as ReadOnly so foreign agents
// with read-only ceilings can still call these tools.
func (h *MCPHandler) RequiredMode(_ string) mcp.PermissionMode {
	return mcp.ModeReadOnly
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "graph_summary":
		return h.handleSummary(ctx, input)
	case "graph_query":
		return h.handleQuery(ctx, input)
	case "graph_node":
		return h.handleNode(ctx, input)
	case "graph_neighbors":
		return h.handleNeighbors(ctx, input)
	case "graph_god_nodes":
		return h.handleGodNodes(ctx, input)
	case "graph_shortest_path":
		return h.handleShortestPath(ctx, input)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("no resources: %s", uri)
}

// loadOrBuild resolves the cwd, loads the cached graph if present, and
// rebuilds otherwise (or when force=true). Centralizes the cache-or-
// rebuild logic so each tool handler stays focused on its query shape.
func loadOrBuild(ctx context.Context, cwd string, force bool) (*GraphContext, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
		}
	}
	cachePath := CachePath(cwd)
	if !force {
		gc, err := Load(cachePath)
		if err == nil && gc != nil {
			return gc, nil
		}
	}
	gc, err := Build(cwd)
	if err != nil {
		return nil, fmt.Errorf("build graph: %w", err)
	}
	// Best-effort cache write — failure is non-fatal.
	_ = gc.Save(cachePath)
	return gc, nil
}

func (h *MCPHandler) handleSummary(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Cwd          string `json:"cwd"`
		ForceRebuild bool   `json:"force_rebuild"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
	}
	gc, err := loadOrBuild(ctx, args.Cwd, args.ForceRebuild)
	if err != nil {
		return "", err
	}
	return gc.Summary(), nil
}

func (h *MCPHandler) handleQuery(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Cwd      string `json:"cwd"`
		Question string `json:"question"`
		Depth    int    `json:"depth"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if args.Question == "" {
		return "", fmt.Errorf("question is required")
	}
	gc, err := loadOrBuild(ctx, args.Cwd, false)
	if err != nil {
		return "", err
	}
	return gc.QueryGraph(args.Question, args.Depth), nil
}

func (h *MCPHandler) handleNode(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Cwd   string `json:"cwd"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if args.Label == "" {
		return "", fmt.Errorf("label is required")
	}
	gc, err := loadOrBuild(ctx, args.Cwd, false)
	if err != nil {
		return "", err
	}
	return gc.GetNode(args.Label), nil
}

func (h *MCPHandler) handleNeighbors(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Cwd            string `json:"cwd"`
		Label          string `json:"label"`
		RelationFilter string `json:"relation_filter"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if args.Label == "" {
		return "", fmt.Errorf("label is required")
	}
	gc, err := loadOrBuild(ctx, args.Cwd, false)
	if err != nil {
		return "", err
	}
	return gc.GetNeighbors(args.Label, args.RelationFilter), nil
}

func (h *MCPHandler) handleGodNodes(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Cwd  string `json:"cwd"`
		TopN int    `json:"top_n"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
	}
	gc, err := loadOrBuild(ctx, args.Cwd, false)
	if err != nil {
		return "", err
	}
	return gc.GetGodNodes(args.TopN), nil
}

func (h *MCPHandler) handleShortestPath(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Cwd     string `json:"cwd"`
		Source  string `json:"source"`
		Target  string `json:"target"`
		MaxHops int    `json:"max_hops"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if args.Source == "" || args.Target == "" {
		return "", fmt.Errorf("source and target are required")
	}
	if args.MaxHops == 0 {
		args.MaxHops = 8
	}
	gc, err := loadOrBuild(ctx, args.Cwd, false)
	if err != nil {
		return "", err
	}
	return gc.ShortestPath(args.Source, args.Target, args.MaxHops), nil
}
