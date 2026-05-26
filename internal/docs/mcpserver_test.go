package docs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// TestMCPHandlerInterface is a compile-time guard. If the
// mcp.ServerHandler interface ever grows a method, this test fails to
// build and we know to update MCPHandler. Same trick for
// PermissionAware — we promise the handler implements it.
func TestMCPHandlerInterface(t *testing.T) {
	var _ mcp.ServerHandler = (*MCPHandler)(nil)
	var _ mcp.PermissionAware = (*MCPHandler)(nil)
}

func TestListToolsAdvertisesBothTools(t *testing.T) {
	tools := NewMCPHandler().ListTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	names := map[string]bool{tools[0].Name: true, tools[1].Name: true}
	for _, want := range []string{"list_docs", "get_doc"} {
		if !names[want] {
			t.Errorf("missing tool %q (got %v)", want, names)
		}
	}
}

func TestListDocsReturnsJSONArray(t *testing.T) {
	h := NewMCPHandler()
	out, err := h.HandleToolCall(context.Background(), "list_docs", nil)
	if err != nil {
		t.Fatalf("list_docs: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("list_docs output is not JSON array: %v\n%s", err, out)
	}
	if len(rows) == 0 {
		t.Fatal("list_docs returned empty array")
	}
	for i, r := range rows {
		for _, field := range []string{"topic", "summary", "when", "max_lines"} {
			if _, ok := r[field]; !ok {
				t.Errorf("row %d missing %q field: %v", i, field, r)
			}
		}
	}
}

func TestGetDocReturnsTopicBody(t *testing.T) {
	h := NewMCPHandler()
	input := json.RawMessage(`{"topic": "mcp"}`)
	out, err := h.HandleToolCall(context.Background(), "get_doc", input)
	if err != nil {
		t.Fatalf("get_doc: %v", err)
	}
	// Raw body starts with the frontmatter fence — proves we returned
	// the full file, not just the body section.
	if !strings.HasPrefix(out, "---\n") {
		t.Fatalf("get_doc output missing frontmatter fence:\n%s", out[:min(200, len(out))])
	}
	if !strings.Contains(out, "## Exact calls") {
		t.Error("get_doc body missing required ## Exact calls section")
	}
}

func TestGetDocEmptyTopicReturnsIndex(t *testing.T) {
	h := NewMCPHandler()

	// Three flavors of "empty" that real MCP clients send.
	cases := []struct {
		name  string
		input json.RawMessage
	}{
		{"nil", nil},
		{"empty-object", json.RawMessage(`{}`)},
		{"explicit-empty-topic", json.RawMessage(`{"topic": ""}`)},
		{"underscore-index", json.RawMessage(`{"topic": "_index"}`)},
	}
	want, err := IndexBody()
	if err != nil {
		t.Fatalf("IndexBody: %v", err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := h.HandleToolCall(context.Background(), "get_doc", c.input)
			if err != nil {
				t.Fatalf("get_doc(%s): %v", c.name, err)
			}
			if out != want {
				t.Errorf("get_doc(%s) did not return IndexBody; first 200 chars:\n%s",
					c.name, out[:min(200, len(out))])
			}
		})
	}
}

func TestGetDocUnknownTopicReportsAvailable(t *testing.T) {
	h := NewMCPHandler()
	input := json.RawMessage(`{"topic": "nonesuch"}`)
	_, err := h.HandleToolCall(context.Background(), "get_doc", input)
	if err == nil {
		t.Fatal("get_doc on unknown topic should error")
	}
	if !strings.Contains(err.Error(), "available:") {
		t.Errorf("error should list available topics, got: %v", err)
	}
}

func TestUnknownToolErrors(t *testing.T) {
	h := NewMCPHandler()
	_, err := h.HandleToolCall(context.Background(), "drop_table_docs", nil)
	if err == nil {
		t.Fatal("unknown tool should error")
	}
}

func TestListResourcesIncludesIndexPlusTopics(t *testing.T) {
	h := NewMCPHandler()
	resources := h.ListResources()
	if len(resources) < 2 {
		t.Fatalf("expected >=2 resources (index + topics), got %d", len(resources))
	}
	// Index always present, always first slot when sorted.
	wantIndex := resourceURI + indexResourceSlug
	found := false
	for _, r := range resources {
		if r.URI == wantIndex {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("index resource %q missing from ListResources()", wantIndex)
	}
}

func TestReadResourceRoundTrip(t *testing.T) {
	h := NewMCPHandler()
	ctx := context.Background()

	// Pick the first topic to read; uses Topics() to avoid hard-coding "mcp"
	// in case the seed doc is renamed.
	topics, err := Topics()
	if err != nil || len(topics) == 0 {
		t.Fatalf("no topics available: %v", err)
	}
	uri := resourceURI + topics[0]
	body, err := h.ReadResource(ctx, uri)
	if err != nil {
		t.Fatalf("ReadResource(%s): %v", uri, err)
	}
	if !strings.HasPrefix(body, "---\n") {
		t.Errorf("ReadResource body missing frontmatter; first 80 chars: %q", body[:min(80, len(body))])
	}
}

func TestReadResourceUnknownScheme(t *testing.T) {
	h := NewMCPHandler()
	_, err := h.ReadResource(context.Background(), "file:///etc/passwd")
	if err == nil {
		t.Fatal("non-ycode:// URI should be rejected")
	}
}

// TestRequiredModeReadOnly is the safeguard #1 enforcement. Both tools
// must report ModeReadOnly so the standalone stdio gate (StaticGate with
// ReadOnly ceiling) keeps them callable, and so any future write-capable
// addition is caught by this test as a regression.
func TestRequiredModeReadOnly(t *testing.T) {
	h := NewMCPHandler()
	for _, name := range []string{"list_docs", "get_doc"} {
		if got := h.RequiredMode(name); got != mcp.ModeReadOnly {
			t.Errorf("%s: want ModeReadOnly, got %v", name, got)
		}
	}
}
