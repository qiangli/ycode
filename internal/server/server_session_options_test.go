package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/service"
)

func TestCreateSession_AcceptsAndEchoesSessionOptions(t *testing.T) {
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })
	svc := &mockService{b: memBus}
	srv := New(Config{}, svc)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	body := struct {
		WorkDir string                  `json:"work_dir"`
		Options *service.SessionOptions `json:"session_options,omitempty"`
	}{
		WorkDir: "/tmp/multi-tenant-x",
		Options: &service.SessionOptions{
			Model:           "claude-haiku-4-5-20251001",
			ToolsAllowlist:  []string{"read_file", "grep_search"},
			ToolsBlocklist:  []string{"bash"},
			PersonaDisabled: true,
		},
	}
	buf, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var info service.SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Options == nil {
		t.Fatal("session_options was lost on the wire")
	}
	if info.Options.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("Model = %q, want claude-haiku-4-5-20251001", info.Options.Model)
	}
	if !info.Options.PersonaDisabled {
		t.Error("PersonaDisabled lost")
	}
	if len(info.Options.ToolsAllowlist) != 2 || info.Options.ToolsAllowlist[0] != "read_file" {
		t.Errorf("ToolsAllowlist = %v", info.Options.ToolsAllowlist)
	}
	if len(info.Options.ToolsBlocklist) != 1 || info.Options.ToolsBlocklist[0] != "bash" {
		t.Errorf("ToolsBlocklist = %v", info.Options.ToolsBlocklist)
	}

	// Assert the service received the options off the request context, not
	// just from the body parse.
	if svc.lastCreateOpts == nil || svc.lastCreateOpts.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("CtxSessionOptions did not reach CreateSession: %+v", svc.lastCreateOpts)
	}
}

func TestCreateSession_OmitsOptionsWhenZero(t *testing.T) {
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })
	svc := &mockService{b: memBus}
	srv := New(Config{}, svc)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	resp, err := http.Post(ts.URL+"/api/sessions", "application/json",
		bytes.NewReader([]byte(`{"work_dir":"/tmp/y"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("got %d, want 201", resp.StatusCode)
	}
	var info service.SessionInfo
	json.NewDecoder(resp.Body).Decode(&info)
	if info.Options != nil {
		t.Errorf("expected nil options when none provided, got %+v", info.Options)
	}
}
