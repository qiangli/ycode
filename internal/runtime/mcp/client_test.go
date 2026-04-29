package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		server, tool, want string
	}{
		{"github", "create_pr", "mcp__github__create_pr"},
		{"my-server", "list_tools", "mcp__my-server__list_tools"},
	}

	for _, tc := range tests {
		got := NormalizeName(tc.server, tc.tool)
		if got != tc.want {
			t.Errorf("NormalizeName(%q, %q) = %q, want %q", tc.server, tc.tool, got, tc.want)
		}
	}
}

func TestParseNormalizedName(t *testing.T) {
	tests := []struct {
		name       string
		wantServer string
		wantTool   string
		wantErr    bool
	}{
		{"mcp__github__create_pr", "github", "create_pr", false},
		{"mcp__my-server__list_tools", "my-server", "list_tools", false},
		{"not_mcp_name", "", "", true},
		{"mcp__nounderscore", "", "", true},
	}

	for _, tc := range tests {
		server, tool, err := ParseNormalizedName(tc.name)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseNormalizedName(%q): err=%v, wantErr=%v", tc.name, err, tc.wantErr)
			continue
		}
		if server != tc.wantServer || tool != tc.wantTool {
			t.Errorf("ParseNormalizedName(%q) = (%q, %q), want (%q, %q)",
				tc.name, server, tool, tc.wantServer, tc.wantTool)
		}
	}
}

func TestRegistryAddGet(t *testing.T) {
	reg := NewRegistry()
	client := NewClient(ServerConfig{Name: "test"})
	reg.Add("test", client)

	got, ok := reg.Get("test")
	if !ok {
		t.Fatal("expected to find client 'test'")
	}
	if got != client {
		t.Error("returned client doesn't match")
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent client")
	}
}

func TestBridgeDiscoverTools(t *testing.T) {
	reg := NewRegistry()

	// Create a client with pre-populated tools (simulating post-Connect state).
	client := NewClient(ServerConfig{Name: "test-server"})
	client.tools = []Tool{
		{Name: "echo", Description: "Echo tool", ServerName: "test-server"},
		{Name: "add", Description: "Add numbers", ServerName: "test-server"},
	}
	reg.Add("test-server", client)

	bridge := NewBridge(reg)
	tools, err := bridge.DiscoverTools(context.Background())
	if err != nil {
		t.Fatalf("DiscoverTools: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// Verify normalized names.
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.NormalizedName] = true
	}
	if !names["mcp__test-server__echo"] {
		t.Error("expected mcp__test-server__echo in discovered tools")
	}
	if !names["mcp__test-server__add"] {
		t.Error("expected mcp__test-server__add in discovered tools")
	}
}

func TestClientCallToolWithoutConnect(t *testing.T) {
	client := NewClient(ServerConfig{Name: "test"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.CallTool(ctx, "test", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error calling tool without connecting")
	}
}

func TestConfigLoadMissing(t *testing.T) {
	// Loading from a nonexistent directory should return empty, not error.
	configs, err := LoadConfig("/nonexistent/path")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected empty configs, got %d", len(configs))
	}
}
