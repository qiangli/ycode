package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const cloudboxOKResponse = `{
  "object": "list",
  "data": [
    {"id": "gpt-5.5", "object": "model", "owned_by": "openai", "x_capabilities": ["chat"], "x_context_length": 200000},
    {"id": "llama3.2:3b", "object": "model", "owned_by": "ollama", "x_capabilities": ["chat"], "x_context_length": 131072},
    {"id": "claude-sonnet-4-6-20250514", "object": "model", "owned_by": "anthropic"},
    {"id": "qwen2.5-coder:32b", "object": "model", "owned_by": "ollama"}
  ]
}`

func TestCloudboxLister_HappyPath(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cloudboxOKResponse))
	}))
	defer srv.Close()

	lister := NewCloudboxLister(srv.URL+"/v1", "tok-abc", srv.Client())
	models := lister(context.Background())

	if got, want := sawAuth, "Bearer tok-abc"; got != want {
		t.Errorf("Authorization header: got %q, want %q", got, want)
	}
	if len(models) != 4 {
		t.Fatalf("expected 4 models, got %d: %+v", len(models), models)
	}

	byID := map[string]ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
		if m.Source != "cloudbox" {
			t.Errorf("model %q source = %q, want cloudbox", m.ID, m.Source)
		}
	}
	if byID["gpt-5.5"].Provider != "openai" {
		t.Errorf("gpt-5.5 provider = %q, want openai", byID["gpt-5.5"].Provider)
	}
	if byID["llama3.2:3b"].Provider != "ollama" {
		t.Errorf("llama3.2:3b provider = %q, want ollama", byID["llama3.2:3b"].Provider)
	}
	if byID["claude-sonnet-4-6-20250514"].Provider != "anthropic" {
		t.Errorf("claude provider = %q, want anthropic", byID["claude-sonnet-4-6-20250514"].Provider)
	}
	if byID["qwen2.5-coder:32b"].Provider != "ollama" {
		t.Errorf("qwen provider = %q, want ollama", byID["qwen2.5-coder:32b"].Provider)
	}
}

func TestCloudboxLister_UnauthorizedReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	lister := NewCloudboxLister(srv.URL+"/v1", "", srv.Client())
	if models := lister(context.Background()); models != nil {
		t.Errorf("expected nil on 401, got %+v", models)
	}
}

func TestCloudboxLister_ForbiddenReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	lister := NewCloudboxLister(srv.URL+"/v1", "bad-token", srv.Client())
	if models := lister(context.Background()); models != nil {
		t.Errorf("expected nil on 403, got %+v", models)
	}
}

func TestCloudboxLister_EmptyTokenNoAuthHeader(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	lister := NewCloudboxLister(srv.URL+"/v1", "", srv.Client())
	_ = lister(context.Background())

	if sawAuth != "" {
		t.Errorf("expected no Authorization header for empty token, got %q", sawAuth)
	}
}

func TestCloudboxLister_TransportErrorReturnsNil(t *testing.T) {
	// Point at a closed listener — connection refuses immediately.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	lister := NewCloudboxLister(url+"/v1", "tok", &http.Client{})
	if models := lister(context.Background()); models != nil {
		t.Errorf("expected nil on transport error, got %+v", models)
	}
}

func TestCloudboxLister_BadJSONReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	lister := NewCloudboxLister(srv.URL+"/v1", "tok", srv.Client())
	if models := lister(context.Background()); models != nil {
		t.Errorf("expected nil on bad JSON, got %+v", models)
	}
}

func TestCloudboxLister_EmptyBaseURLUsesDefault(t *testing.T) {
	// We can't actually hit ai.dhnt.io in a unit test; we just confirm the
	// constructor accepts an empty baseURL without panicking and yields
	// SOMETHING callable. The call itself will fail (no network or 401) and
	// return nil — that's fine; this guards against a nil-deref regression.
	lister := NewCloudboxLister("", "", nil)
	if lister == nil {
		t.Fatal("expected non-nil lister")
	}
	if !strings.Contains(DefaultCloudboxURL, "ai.dhnt.io") {
		t.Errorf("DefaultCloudboxURL changed unexpectedly: %q", DefaultCloudboxURL)
	}
}

func TestDiscoverCloudboxOnly_NilLister(t *testing.T) {
	models := DiscoverCloudboxOnly(context.Background(), nil)
	if models == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(models) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(models))
	}
}

func TestDiscoverCloudboxOnly_PassesThrough(t *testing.T) {
	want := []ModelInfo{{ID: "x", Provider: "openai", Source: "cloudbox"}}
	lister := CloudboxLister(func(ctx context.Context) []ModelInfo { return want })
	got := DiscoverCloudboxOnly(context.Background(), lister)
	if len(got) != 1 || got[0].ID != "x" {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDiscoverEnvAndCloudbox_MergesEnvAndCloudbox(t *testing.T) {
	// Clear other env keys so we only test OPENAI_API_KEY's models.
	for _, k := range []string{
		"ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "GEMINI_API_KEY",
		"XAI_API_KEY", "DASHSCOPE_API_KEY", "MOONSHOT_API_KEY", "KIMI_API_KEY",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("OPENAI_API_KEY", "sk-test")

	lister := CloudboxLister(func(ctx context.Context) []ModelInfo {
		return []ModelInfo{
			{ID: "qwen2.5-coder:32b", Provider: "ollama", Source: "cloudbox"},
			// gpt-5.5 overlaps with the OPENAI_API_KEY env-detected list:
			// env wins (appears first), cloudbox entry is deduped.
			{ID: "gpt-5.5", Provider: "openai", Source: "cloudbox"},
		}
	})

	models := DiscoverEnvAndCloudbox(context.Background(), lister)

	bySource := map[string]int{}
	byID := map[string]ModelInfo{}
	for _, m := range models {
		bySource[m.Source]++
		byID[m.ID] = m
	}
	if bySource["env"] == 0 {
		t.Errorf("expected env-detected models, got sources: %+v", bySource)
	}
	if bySource["cloudbox"] == 0 {
		t.Errorf("expected cloudbox models, got sources: %+v", bySource)
	}
	// gpt-5.5 came from BOTH env and cloudbox; env wins on dedup.
	if byID["gpt-5.5"].Source != "env" {
		t.Errorf("gpt-5.5 source = %q, want env (env appears first and dedupes cloudbox)", byID["gpt-5.5"].Source)
	}
	// qwen unique to cloudbox.
	if byID["qwen2.5-coder:32b"].Source != "cloudbox" {
		t.Errorf("qwen source = %q, want cloudbox", byID["qwen2.5-coder:32b"].Source)
	}
	// Built-in aliases (opus/sonnet/haiku) must NOT appear.
	for _, banned := range []string{"opus", "sonnet", "haiku"} {
		for _, m := range models {
			if m.Alias == banned {
				t.Errorf("alias %q should not appear in env+cloudbox picker; got %+v", banned, m)
			}
		}
	}
}

func TestDiscoverEnvAndCloudbox_NilLister(t *testing.T) {
	// All env keys cleared; nil lister.
	for _, k := range []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GOOGLE_API_KEY", "GEMINI_API_KEY",
		"XAI_API_KEY", "DASHSCOPE_API_KEY", "MOONSHOT_API_KEY", "KIMI_API_KEY",
		"DEEPSEEK_API_KEY",
	} {
		t.Setenv(k, "")
	}
	models := DiscoverEnvAndCloudbox(context.Background(), nil)
	if len(models) != 0 {
		t.Errorf("expected empty result with no env keys + nil lister, got %+v", models)
	}
}
