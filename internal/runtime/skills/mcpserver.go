// Package skills exposes ycode's universal skills (currently /foreman)
// as MCP tools and resources so foreign agents can discover and read
// skill bodies via the standardized Model Context Protocol — without
// ycode writing into ~/.claude/skills/, ~/.codex/, etc.
//
// Pull surface only: agents that auto-spawn `ycode mcp serve` via the
// .mcp.json lighthouse beam call list_skills / get_skill (or read the
// matching ycode://skills/<name> resource). The skill body is resolved
// from the four-tier ladder: cwd → project → user (~/.config/ycode/) →
// embedded.
package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/selfinit"
)

// MCPHandler implements mcp.ServerHandler, exposing ycode's universal
// skill inventory as MCP tools and resources.
type MCPHandler struct{}

// NewMCPHandler returns a handler ready to mount in
// mcp.NewCompositeHandler(...).
func NewMCPHandler() *MCPHandler { return &MCPHandler{} }

// resourceURI is the canonical scheme for skill resources.
const resourceURI = "ycode://skills/"

// ListTools surfaces two tools:
//   - list_skills  — return the skill inventory as JSON
//   - get_skill    — return the body of one skill by name
func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "list_skills",
			Description: "List ycode's universal skills (e.g. /foreman). Each entry " +
				"includes name, summary, when-to-use, and the canonical body path. " +
				"Use this to discover what procedural knowledge ycode ships with the " +
				"binary; follow up with get_skill to fetch the body.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name: "get_skill",
			Description: "Return the markdown body of one ycode skill. The body is " +
				"resolved via the four-tier ladder (cwd → project .agents/ycode/ → " +
				"user ~/.config/ycode/ → embedded). Pass the skill name with or " +
				"without a leading slash (e.g. 'foreman' or '/foreman').",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name": {"type": "string", "description": "Skill name, with or without leading slash."}
				},
				"required": ["name"]
			}`),
		},
	}
}

// ListResources exposes one resource per universal skill at
// ycode://skills/<name>. Foreign agents that prefer the resources
// abstraction over tool calls can resources/list and resources/read
// the same content.
func (h *MCPHandler) ListResources() []mcp.Resource {
	out := make([]mcp.Resource, 0, len(selfinit.SkillInventory))
	for _, s := range selfinit.SkillInventory {
		name := strings.TrimPrefix(s.Name, "/")
		out = append(out, mcp.Resource{
			URI:         resourceURI + name,
			Name:        s.Name,
			Description: s.Summary + ". " + s.When,
			MimeType:    "text/markdown",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out
}

// HandleToolCall dispatches list_skills and get_skill.
func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "list_skills":
		entries := make([]map[string]string, 0, len(selfinit.SkillInventory))
		for _, s := range selfinit.SkillInventory {
			entries = append(entries, map[string]string{
				"name":      s.Name,
				"summary":   s.Summary,
				"when":      s.When,
				"body_path": s.BodyPath,
			})
		}
		out, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return "", err
		}
		return string(out), nil

	case "get_skill":
		var args struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("get_skill: parse args: %w", err)
		}
		if args.Name == "" {
			return "", fmt.Errorf("get_skill: name is required")
		}
		body, _, err := ResolveSkillBody(args.Name)
		if err != nil {
			return "", err
		}
		return body, nil

	default:
		return "", fmt.Errorf("unknown tool: %q", name)
	}
}

// ReadResource serves ycode://skills/<name>.
func (h *MCPHandler) ReadResource(ctx context.Context, uri string) (string, error) {
	if !strings.HasPrefix(uri, resourceURI) {
		return "", fmt.Errorf("unknown resource URI: %q", uri)
	}
	name := strings.TrimPrefix(uri, resourceURI)
	body, _, err := ResolveSkillBody(name)
	return body, err
}

// ResolveSkillBody returns the markdown body for the named skill plus
// the source it was resolved from ("cwd" | "project" | "user" |
// "embedded"). Resolution order:
//
//  1. <cwd>/.agents/ycode/skills/ycode-<name>/skill.md
//  2. <repo>/.agents/ycode/skills/ycode-<name>/skill.md (if cwd != repo)
//  3. ~/.config/ycode/skills/ycode-<name>/skill.md
//  4. embedded canonical (built-in)
//
// First match wins. Returns an error only if all four miss AND the
// name doesn't match any known universal skill.
func ResolveSkillBody(name string) (body, source string, err error) {
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return "", "", fmt.Errorf("empty skill name")
	}
	dirName := name
	if !strings.HasPrefix(dirName, "ycode-") {
		dirName = "ycode-" + dirName
	}

	// 1. cwd-local override.
	if cwd, err := os.Getwd(); err == nil {
		p := filepath.Join(cwd, ".agents", "ycode", "skills", dirName, "skill.md")
		if data, err := os.ReadFile(p); err == nil {
			return string(data), "cwd", nil
		}
	}

	// 2. user-global.
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".config", "ycode", "skills", dirName, "skill.md")
		if data, err := os.ReadFile(p); err == nil {
			return string(data), "user", nil
		}
	}

	// 3. embedded fallback. Currently only /foreman is embedded.
	switch name {
	case "foreman":
		return selfinit.ForemanSkillBody(), "embedded", nil
	}

	return "", "", fmt.Errorf("skill not found: %q", name)
}
