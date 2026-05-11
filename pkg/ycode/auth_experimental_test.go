//go:build experimental

package ycode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/tools"
	"github.com/qiangli/ycode/pkg/ycode/actor"
)

func TestHandlerWithAuth_MiddlewareInvoked(t *testing.T) {
	a, err := NewAgent(WithProvider(newStubProvider(api.ProviderOpenAI)), WithoutBuiltinTools())
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	calls := 0
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			ctx := actor.WithUser(r.Context(), actor.User{
				ID: "u1", Roles: []string{"admin"},
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	srv := httptest.NewServer(a.HandlerWithAuth(mw))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	defer resp.Body.Close()
	if calls != 1 {
		t.Errorf("middleware invocation count: want 1, got %d", calls)
	}
}

func TestHandlerWithAuth_NilMiddlewareReturnsBaseMux(t *testing.T) {
	a, err := NewAgent(WithProvider(newStubProvider(api.ProviderOpenAI)), WithoutBuiltinTools())
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	h := a.HandlerWithAuth(nil)
	if h == nil {
		t.Fatal("HandlerWithAuth(nil) should return the base mux, got nil")
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	resp.Body.Close()
}

func TestHandlerWithAuth_HandlerAndHandlerWithAuthShareService(t *testing.T) {
	// Both entry points must use the cached service so per-session state is
	// coherent across mounts. We verify by calling each once and asserting
	// the Agent's cachedSvc is the same after both.
	a, err := NewAgent(WithProvider(newStubProvider(api.ProviderOpenAI)), WithoutBuiltinTools())
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	_ = a.Handler() // first call initializes
	svc1 := a.cachedSvc

	_ = a.HandlerWithAuth(nil) // second call must reuse, not replace
	svc2 := a.cachedSvc

	if svc1 == nil || svc2 == nil {
		t.Fatal("cachedSvc not initialized")
	}
	if svc1 != svc2 {
		t.Error("Handler() and HandlerWithAuth() built separate services — caching regressed")
	}
}

// Round-trip the actor.User from an http middleware → request context →
// custom tool handler retrieved via the Registry. Proves end-to-end that
// the contract documented in pkg/ycode/actor is honored by the embedding
// surface.
func TestActorContextReachesCustomToolHandler(t *testing.T) {
	a, err := NewAgent(WithProvider(newStubProvider(api.ProviderOpenAI)), WithoutBuiltinTools())
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	gotUser := make(chan actor.User, 1)
	err = a.Registry().Register(&tools.ToolSpec{
		Name:         "classgo.who",
		Description:  "Domain tool that reads actor.User from ctx.",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		RequiredMode: permission.ReadOnly,
		Handler: func(ctx context.Context, _ json.RawMessage) (string, error) {
			if u, ok := actor.UserFromContext(ctx); ok {
				gotUser <- u
			}
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Simulate the http path's effect on ctx: middleware stamps the user.
	ctx := actor.WithUser(context.Background(), actor.User{
		ID:    "u42",
		Roles: []string{"admin"},
	})

	if _, err := a.Registry().Invoke(ctx, "classgo.who", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	select {
	case u := <-gotUser:
		if u.ID != "u42" {
			t.Errorf("user ID mismatch: want u42, got %q", u.ID)
		}
		if !actor.HasRole(actor.WithUser(context.Background(), u), "admin") {
			t.Error("HasRole(admin) returned false on round-tripped user")
		}
	default:
		t.Error("custom tool handler did not observe actor.User on ctx")
	}
}
