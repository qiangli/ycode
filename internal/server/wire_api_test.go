package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/service"
	"github.com/qiangli/ycode/pkg/ycode/actor"
)

// TestWireAPI_FullPath chains the four pieces classgo (and other
// third-party hosts) need: bearer auth, actor header decoding, workDir
// resolution, and a stateless extract call. Today these all live behind
// the same `authMiddleware`; this test would catch a regression that
// silently disabled any of them.
func TestWireAPI_FullPath(t *testing.T) {
	const token = "wire-api-token"

	stub := &stubExtractProvider{kind: api.ProviderOpenAI, emitted: `{"ok":true}`}

	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })
	app := &fakeAppForExtract{
		provider: stub,
		cfg:      &config.Config{Model: "wire-test-model", MaxTokens: 1024},
	}
	svc := &mockServiceWithApp{
		mockService: &mockService{b: memBus},
		app:         app,
	}
	srv := New(Config{Token: token}, svc)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	body := extractRequest{
		Prompt: "produce JSON",
		Schema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`),
	}
	buf, _ := json.Marshal(body)

	t.Run("BearerMissing_401", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/extract", bytes.NewReader(buf))
		req.Header.Set("X-Work-Dir", "/tmp/x")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("got %d, want 401", resp.StatusCode)
		}
	})

	t.Run("BearerOK_ActorOK_ExtractOK", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/extract", bytes.NewReader(buf))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Actor-User", "parent_47")
		req.Header.Set("X-Actor-Roles", "parent")
		req.Header.Set("X-Work-Dir", "/tmp/x")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d, want 200", resp.StatusCode)
		}
		var got map[string]bool
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if !got["ok"] {
			t.Errorf("response ok = %v, want true", got["ok"])
		}
		if stub.lastReq == nil || stub.lastReq.Model != "wire-test-model" {
			t.Errorf("provider didn't receive expected model: %+v", stub.lastReq)
		}
	})
}

// TestWireAPI_ActorPropagation proves end-to-end that an actor identity
// supplied via headers + valid bearer reaches a downstream handler's
// request context. The probe handler in TestActorHeaderDecoding does the
// same, but this test layers the handler underneath authMiddleware in
// the same registration pattern as production endpoints.
func TestWireAPI_ActorPropagation(t *testing.T) {
	const token = "wire-actor-token"

	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })
	svc := &mockService{b: memBus}
	srv := New(Config{Token: token}, svc)

	// Probe handler installed under the same auth middleware production
	// uses; capture the actor.User to prove propagation.
	probed := make(chan actor.User, 1)
	srv.mux.HandleFunc("GET /api/_wire_probe", srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		u, _ := actor.UserFromContext(r.Context())
		probed <- u
		w.WriteHeader(http.StatusOK)
	}))

	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/_wire_probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Actor-User", "parent_47")
	req.Header.Set("X-Actor-Roles", "parent,reader")
	req.Header.Set("X-Actor-Email", "p47@example.com")
	req.Header.Set("X-Actor-Extra-Tenant", "school-12")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}

	select {
	case u := <-probed:
		if u.ID != "parent_47" || u.Email != "p47@example.com" {
			t.Errorf("user ID/email mismatch: %+v", u)
		}
		if !actor.HasRole(actorCtx(u), "parent") {
			t.Error("HasRole(parent) returned false")
		}
		if u.Extra["tenant"] != "school-12" {
			t.Errorf("Extra[tenant] = %q", u.Extra["tenant"])
		}
	default:
		t.Fatal("probe handler never invoked")
	}
}

// actorCtx is a tiny helper used only by TestWireAPI_ActorPropagation to
// run actor.HasRole against a User without a request available.
func actorCtx(u actor.User) context.Context {
	return actor.WithUser(context.Background(), u)
}

// Compile-time assertion that the Service interface still satisfies what
// MultiService and LocalService implement — catches accidental drift
// during refactors that touch the interface or its implementors.
var (
	_ service.Service = (*service.LocalService)(nil)
	_ service.Service = (*service.MultiService)(nil)
)
