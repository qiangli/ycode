// Package memexmcp exposes ycode's persistent agent memory (pkg/memex/memory)
// to external coding agents over MCP. Foreign agents like Claude Code, Codex,
// Aider, Gemini CLI, and opencode all rely on flat CLAUDE.md / AGENTS.md
// files for cross-session memory; this family gives them ycode's structured
// alternative — RRF-fused search across name/description/content,
// scope-aware writes, soft-delete via Forget, and an index dump for cold
// orientation.
//
// Capability family A.3 in docs/lighthouse-roadmap.md. Lives in
// internal/runtime to keep pkg/memex/memory free of internal imports;
// new tools plug in here exactly the way internal/runtime/treesitter's
// MCP handler plugs in for AST capabilities.
package memexmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/pkg/memex/memory"
)

// MCPHandler bridges ycode's memex memory Manager to MCP. Construct
// with the same Manager wiring that main.go uses (NewManagerWithGlobal
// rooted at ~/.agents/ycode/memory/ + {cwd}/.agents/ycode/memory/).
//
// The handler does not own the Manager's lifecycle — callers Close()
// theirs the usual way on shutdown.
type MCPHandler struct {
	mgr *memory.Manager
}

// NewMCPHandler returns a handler backed by mgr. Panics if mgr is nil
// because the handler is useless without a backing manager and silent
// no-ops on every tool call would be worse than a clear panic at
// composite-construction time.
func NewMCPHandler(mgr *memory.Manager) *MCPHandler {
	if mgr == nil {
		panic("memexmcp.NewMCPHandler: nil memory.Manager")
	}
	return &MCPHandler{mgr: mgr}
}

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "memex_recall",
			Description: "Search ycode's persistent agent memory for entries relevant to a query. " +
				"Uses Reciprocal Rank Fusion across name/description/content (plus Bleve and vector " +
				"backends when wired). Returns a JSON array of {name, description, type, scope, " +
				"importance, score, source, host, project_id} records, sorted best-first. The " +
				"optional `scope` argument restricts results to a single scope band — useful for " +
				"cross-repo queries like \"recall something I worked on in another repo\" via " +
				"scope=\"user\".",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query":       {"type": "string", "description": "Free-text query. Names, themes, and concepts all hit."},
					"max_results": {"type": "integer", "description": "Cap on returned records. Default 10, max 50."},
					"scope":       {"type": "string", "description": "Optional filter: project | user | global | team | all. Defaults to all (cross-scope cascade with project boosted, user/team attenuated, global most attenuated)."}
				},
				"required": ["query"]
			}`),
		},
		{
			Name: "memex_save",
			Description: "Persist a new memory or overwrite an existing one by name. type must be one of: " +
				"user, feedback, project, reference, episodic, procedural, task. scope is optional and " +
				"defaults to project (other values: global, user, team). description is a short label " +
				"shown in indexes; content is the full body (markdown is fine). importance is 0.0-1.0 " +
				"and influences recall scoring.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name":        {"type": "string"},
					"type":        {"type": "string", "description": "user | feedback | project | reference | episodic | procedural | task"},
					"scope":       {"type": "string", "description": "Optional. global | project | user | team. Defaults to project."},
					"description": {"type": "string"},
					"content":     {"type": "string"},
					"importance":  {"type": "number", "description": "Optional 0.0-1.0. Defaults to 0.5."},
					"tags":        {"type": "array", "items": {"type": "string"}, "description": "Optional free-form tags."}
				},
				"required": ["name", "type", "description", "content"]
			}`),
		},
		{
			Name:        "memex_list",
			Description: "Return every memory across all scopes as a JSON array. Useful for orientation, audits, or seeding a downstream agent's context. Heavyweight on large memory sets — prefer memex_recall when you have a query.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        "memex_forget",
			Description: "Remove a memory by name (across all scopes). Returns an error if no memory with that name exists. Irreversible.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`),
		},
		{
			Name:        "memex_index",
			Description: "Read the MEMORY.md index file — the canonical entry-point for orientation. Returns the markdown verbatim.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name: "search_memex",
			Description: "Search memex with optional backend selection. Wraps memex_recall and post-filters " +
				"results by the Source field — useful when the agent wants only Bleve full-text hits, only " +
				"vector matches, or only keyword/entity matches. Accepts the same query and max_results " +
				"as memex_recall plus a sources[] filter. Returns the same JSON shape as memex_recall.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query":       {"type": "string", "description": "Free-text query."},
					"max_results": {"type": "integer", "description": "Cap on returned records. Default 10, max 50."},
					"sources":     {"type": "array", "items": {"type": "string"}, "description": "Optional. Only return records whose Source matches one of these. Common values: vector, bleve, keyword."}
				},
				"required": ["query"]
			}`),
		},
		{
			Name:        "list_memory_types",
			Description: "Return the seven canonical memory types ycode supports: user, feedback, project, reference, episodic, procedural, task. Useful for foreign agents that need to validate the `type` argument before calling memex_save.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

// RequiredMode classifies each tool. Recall/list/index are ReadOnly;
// save/forget mutate the project memory tree on disk and so require
// WorkspaceWrite. The composite's GatedHandler enforces these per
// tool — see docs/lighthouse.md.
func (h *MCPHandler) RequiredMode(tool string) mcp.PermissionMode {
	switch tool {
	case "memex_save", "memex_forget":
		return mcp.ModeWorkspaceWrite
	default:
		return mcp.ModeReadOnly
	}
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "memex_recall":
		return h.handleRecall(ctx, input)
	case "memex_save":
		return h.handleSave(ctx, input)
	case "memex_list":
		return h.handleList()
	case "memex_forget":
		return h.handleForget(input)
	case "memex_index":
		return h.handleIndex()
	case "search_memex":
		return h.handleSearch(ctx, input)
	case "list_memory_types":
		return h.handleListTypes()
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("no resources: %s", uri)
}

// recallView is the projection memex_recall returns. It deliberately
// omits FilePath and other on-disk metadata so the foreign agent
// can't leak host paths through its own logs/UI. Host and ProjectID
// from Origin are included so cross-host/cross-repo provenance is
// observable to the agent (they're identifiers, not paths).
type recallView struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Type        string  `json:"type"`
	Scope       string  `json:"scope"`
	Importance  float64 `json:"importance"`
	Score       float64 `json:"score"`
	Source      string  `json:"source,omitempty"`
	Host        string  `json:"host,omitempty"`
	ProjectID   string  `json:"project_id,omitempty"`
	Content     string  `json:"content"`
}

const (
	defaultRecallResults = 10
	maxRecallResults     = 50
)

func (h *MCPHandler) handleRecall(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
		Scope      string `json:"scope"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", fmt.Errorf("query is required")
	}
	if args.MaxResults <= 0 {
		args.MaxResults = defaultRecallResults
	}
	if args.MaxResults > maxRecallResults {
		args.MaxResults = maxRecallResults
	}

	scopeFilter, err := parseScopeFilter(args.Scope)
	if err != nil {
		return "", err
	}

	// When the caller asks for a single scope, widen the underlying recall
	// so the post-filter still leaves enough hits to satisfy MaxResults.
	internalCap := args.MaxResults
	if scopeFilter != "" {
		internalCap = args.MaxResults * 4
		if internalCap > maxRecallResults*4 {
			internalCap = maxRecallResults * 4
		}
	}

	results, err := h.mgr.Recall(args.Query, internalCap)
	if err != nil {
		return "", fmt.Errorf("recall: %w", err)
	}

	views := make([]recallView, 0, len(results))
	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		if scopeFilter != "" && r.Memory.EffectiveScope() != scopeFilter {
			continue
		}
		v := recallView{
			Name:        r.Memory.Name,
			Description: r.Memory.Description,
			Type:        string(r.Memory.Type),
			Scope:       string(r.Memory.EffectiveScope()),
			Importance:  r.Memory.Importance,
			Score:       r.Score,
			Source:      r.Source,
			Content:     r.Memory.Content,
		}
		if r.Memory.Origin != nil {
			v.Host = r.Memory.Origin.Host
			v.ProjectID = r.Memory.Origin.ProjectID
		}
		views = append(views, v)
		if len(views) >= args.MaxResults {
			break
		}
	}
	out, err := json.Marshal(views)
	if err != nil {
		return "", fmt.Errorf("marshal results: %w", err)
	}
	return string(out), nil
}

// parseScopeFilter turns a raw scope argument into a memory.Scope value
// suitable for direct comparison against Memory.EffectiveScope(). Empty
// input and "all" both mean "no filter" (the empty Scope sentinel).
func parseScopeFilter(raw string) (memory.Scope, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "all":
		return "", nil
	case string(memory.ScopeProject):
		return memory.ScopeProject, nil
	case string(memory.ScopeUser):
		return memory.ScopeUser, nil
	case string(memory.ScopeGlobal):
		return memory.ScopeGlobal, nil
	case string(memory.ScopeTeam):
		return memory.ScopeTeam, nil
	default:
		return "", fmt.Errorf("unknown scope %q (allowed: project, user, global, team, all)", raw)
	}
}

func (h *MCPHandler) handleSave(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Name        string   `json:"name"`
		Type        string   `json:"type"`
		Scope       string   `json:"scope"`
		Description string   `json:"description"`
		Content     string   `json:"content"`
		Importance  float64  `json:"importance"`
		Tags        []string `json:"tags"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if args.Name == "" || args.Type == "" || args.Description == "" || args.Content == "" {
		return "", fmt.Errorf("name, type, description, and content are required")
	}

	// Safety: foreign agents calling over MCP can only write to project
	// scope. Global/user/team are user-driven operations and should be
	// performed through the ycode TUI/CLI by the operator directly. The
	// empty-scope default resolves to ScopeProject via EffectiveScope.
	switch memory.Scope(args.Scope) {
	case "", memory.ScopeProject:
		// allowed
	case memory.ScopeGlobal, memory.ScopeUser, memory.ScopeTeam:
		return "", fmt.Errorf("scope %q is not allowed over MCP — use ycode CLI/TUI for non-project writes", args.Scope)
	default:
		return "", fmt.Errorf("unknown scope %q (allowed: project)", args.Scope)
	}

	importance := args.Importance
	if importance == 0 {
		importance = 0.5
	}

	now := time.Now().UTC()
	mem := &memory.Memory{
		Name:        args.Name,
		Description: args.Description,
		Type:        memory.Type(args.Type),
		Scope:       memory.Scope(args.Scope),
		Content:     args.Content,
		Importance:  importance,
		Tags:        args.Tags,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.mgr.Save(mem); err != nil {
		return "", fmt.Errorf("save: %w", err)
	}
	return fmt.Sprintf(`{"ok":true,"name":%q,"scope":%q}`, mem.Name, mem.EffectiveScope()), nil
}

func (h *MCPHandler) handleSearch(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Query      string   `json:"query"`
		MaxResults int      `json:"max_results"`
		Sources    []string `json:"sources"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", fmt.Errorf("query is required")
	}
	if args.MaxResults <= 0 {
		args.MaxResults = defaultRecallResults
	}
	if args.MaxResults > maxRecallResults {
		args.MaxResults = maxRecallResults
	}

	results, err := h.mgr.Recall(args.Query, args.MaxResults)
	if err != nil {
		return "", fmt.Errorf("recall: %w", err)
	}

	allow := make(map[string]bool, len(args.Sources))
	for _, s := range args.Sources {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			allow[s] = true
		}
	}

	views := make([]recallView, 0, len(results))
	for _, r := range results {
		if r.Memory == nil {
			continue
		}
		if len(allow) > 0 && !allow[strings.ToLower(r.Source)] {
			continue
		}
		views = append(views, recallView{
			Name:        r.Memory.Name,
			Description: r.Memory.Description,
			Type:        string(r.Memory.Type),
			Scope:       string(r.Memory.EffectiveScope()),
			Importance:  r.Memory.Importance,
			Score:       r.Score,
			Source:      r.Source,
			Content:     r.Memory.Content,
		})
	}
	out, err := json.Marshal(views)
	if err != nil {
		return "", fmt.Errorf("marshal results: %w", err)
	}
	return string(out), nil
}

func (h *MCPHandler) handleListTypes() (string, error) {
	types := []memory.Type{
		memory.TypeUser,
		memory.TypeFeedback,
		memory.TypeProject,
		memory.TypeReference,
		memory.TypeEpisodic,
		memory.TypeProcedural,
		memory.TypeTask,
	}
	strs := make([]string, len(types))
	for i, t := range types {
		strs[i] = string(t)
	}
	out, err := json.Marshal(strs)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return string(out), nil
}

func (h *MCPHandler) handleList() (string, error) {
	mems, err := h.mgr.All()
	if err != nil {
		return "", fmt.Errorf("list: %w", err)
	}
	views := make([]recallView, 0, len(mems))
	for _, m := range mems {
		if m == nil {
			continue
		}
		views = append(views, recallView{
			Name:        m.Name,
			Description: m.Description,
			Type:        string(m.Type),
			Scope:       string(m.EffectiveScope()),
			Importance:  m.Importance,
			Content:     m.Content,
		})
	}
	out, err := json.Marshal(views)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return string(out), nil
}

func (h *MCPHandler) handleForget(input json.RawMessage) (string, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if args.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	if err := h.mgr.Forget(args.Name); err != nil {
		return "", fmt.Errorf("forget: %w", err)
	}
	return fmt.Sprintf(`{"ok":true,"forgot":%q}`, args.Name), nil
}

func (h *MCPHandler) handleIndex() (string, error) {
	idx, err := h.mgr.ReadIndex()
	if err != nil {
		return "", fmt.Errorf("read index: %w", err)
	}
	return idx, nil
}
