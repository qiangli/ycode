package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// framingStubHandler is a minimal ServerHandler for framing tests.
type framingStubHandler struct {
	tools []Tool
}

func (h framingStubHandler) HandleToolCall(_ context.Context, name string, _ json.RawMessage) (string, error) {
	return "called " + name, nil
}
func (h framingStubHandler) ListTools() []Tool         { return h.tools }
func (h framingStubHandler) ListResources() []Resource { return nil }
func (h framingStubHandler) ReadResource(_ context.Context, _ string) (string, error) {
	return "", nil
}

// TestRunServerNDJSONFraming pins the on-the-wire framing: Claude Code,
// Cursor, and the reference TS SDK all speak newline-delimited JSON
// over stdio. This test drives runServerOn with NDJSON and asserts an
// NDJSON response — without it, the server would have hung waiting for
// an LSP-style `Content-Length:` header that never came (the bug we
// fixed in this commit).
func TestRunServerNDJSONFraming(t *testing.T) {
	handler := framingStubHandler{tools: []Tool{{Name: "ping", Description: "ping"}}}

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n")
	var out bytes.Buffer

	if err := runServerOn(context.Background(), handler, in, &out); err != nil {
		t.Fatalf("runServerOn: %v", err)
	}

	// Two responses, each terminated by `\n`, no Content-Length headers.
	lines := splitNonEmpty(out.String())
	if len(lines) != 2 {
		t.Fatalf("want 2 response lines, got %d:\n%q", len(lines), out.String())
	}
	if strings.Contains(out.String(), "Content-Length") {
		t.Fatalf("response unexpectedly contained Content-Length header (LSP framing leaked):\n%q", out.String())
	}

	var initResp JSONRPCResponse
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatalf("decode init resp: %v", err)
	}
	if initResp.ID != 1 {
		t.Errorf("init id: want 1, got %d", initResp.ID)
	}
	if initResp.Error != nil {
		t.Errorf("init unexpectedly errored: %+v", initResp.Error)
	}

	var listResp JSONRPCResponse
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatalf("decode list resp: %v", err)
	}
	if listResp.ID != 2 {
		t.Errorf("list id: want 2, got %d", listResp.ID)
	}
	// The result should embed our stub tool.
	if !strings.Contains(string(listResp.Result), `"ping"`) {
		t.Errorf("tools/list missing stub tool:\n%s", listResp.Result)
	}
}

// TestRunServerToleratesBlankLines guards the defensive blank-line
// handling — some clients flush an extra `\n` between messages.
func TestRunServerToleratesBlankLines(t *testing.T) {
	in := strings.NewReader("\n" + `{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}` + "\n\n")
	var out bytes.Buffer
	if err := runServerOn(context.Background(), framingStubHandler{}, in, &out); err != nil {
		t.Fatalf("runServerOn: %v", err)
	}
	lines := splitNonEmpty(out.String())
	if len(lines) != 1 {
		t.Fatalf("want 1 response, got %d:\n%q", len(lines), out.String())
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != 7 {
		t.Errorf("id: want 7, got %d", resp.ID)
	}
}

// TestRunServerEmptyToolsAndResourcesAreArrays guards against the
// nil-slice → JSON null marshal that previously made Claude Code fail
// with `"expected array, received null"` on servers exposing no tools
// or no resources. The server must always emit `[]`, not `null`.
func TestRunServerEmptyToolsAndResourcesAreArrays(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"resources/list","params":{}}` + "\n")
	var out bytes.Buffer
	if err := runServerOn(context.Background(), framingStubHandler{}, in, &out); err != nil {
		t.Fatalf("runServerOn: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, `"tools":[]`) {
		t.Errorf("tools/list did not emit []; got:\n%s", body)
	}
	if !strings.Contains(body, `"resources":[]`) {
		t.Errorf("resources/list did not emit []; got:\n%s", body)
	}
	if strings.Contains(body, `"tools":null`) || strings.Contains(body, `"resources":null`) {
		t.Errorf("nil-slice leaked as JSON null:\n%s", body)
	}
}

// TestRunServerParseError emits a -32700 reply (NDJSON) when the input
// isn't valid JSON, instead of crashing or hanging.
func TestRunServerParseError(t *testing.T) {
	in := strings.NewReader("not-json\n")
	var out bytes.Buffer
	if err := runServerOn(context.Background(), framingStubHandler{}, in, &out); err != nil {
		t.Fatalf("runServerOn: %v", err)
	}
	lines := splitNonEmpty(out.String())
	if len(lines) != 1 {
		t.Fatalf("want 1 reply, got %d:\n%q", len(lines), out.String())
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Errorf("want parse-error reply, got %+v", resp.Error)
	}
}

func splitNonEmpty(s string) []string {
	var out []string
	for l := range strings.SplitSeq(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
