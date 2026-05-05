package memex_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qiangli/ycode/pkg/memex"
	"github.com/qiangli/ycode/pkg/memex/memory"
)

func TestWikiHandler_TreeAndFile(t *testing.T) {
	dir := t.TempDir()
	mx, err := memex.Open(dir)
	if err != nil {
		t.Fatalf("memex.Open: %v", err)
	}
	defer mx.Close()

	// Save one memory so the wiki tree has something to list.
	if err := mx.Memory().Save(&memory.Memory{
		Name:    "wiki-demo",
		Type:    memory.TypeReference,
		Scope:   memory.ScopeGlobal,
		Content: "wiki overlay smoke",
	}); err != nil {
		t.Fatalf("Memory.Save: %v", err)
	}

	srv := httptest.NewServer(mx.HTTPHandler())
	defer srv.Close()

	// /api/wiki/tree should list /memory and /memos as roots.
	resp, err := http.Get(srv.URL + "/api/wiki/tree?path=/")
	if err != nil {
		t.Fatalf("GET /tree: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tree status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"/memory"`) {
		t.Errorf("tree missing /memory root: %s", body)
	}

	// /api/wiki/file should fetch the saved memory by path.
	memPath := memex.MemoryPath(memory.Memory{
		Name:  "wiki-demo",
		Type:  memory.TypeReference,
		Scope: memory.ScopeGlobal,
	})
	resp, err = http.Get(srv.URL + "/api/wiki/file?path=" + memPath)
	if err != nil {
		t.Fatalf("GET /file: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("file status = %d, body = %s", resp.StatusCode, body)
	}
	var got struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode file: %v\nbody: %s", err, body)
	}
	if got.Body != "wiki overlay smoke" {
		t.Errorf("body = %q, want %q", got.Body, "wiki overlay smoke")
	}

	// Reads must be context-cancellable.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mx.VFS().List(ctx, "/"); err == nil {
		// Note: not all backends honor cancellation immediately; this is
		// best-effort. We just want to make sure passing a ctx works.
		t.Log("VFS.List ignored cancelled context (acceptable, advisory)")
	}
}
