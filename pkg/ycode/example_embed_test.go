package ycode_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/embedding"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/tools"
	"github.com/qiangli/ycode/pkg/ycode"
	"github.com/qiangli/ycode/pkg/ycode/actor"
)

// stubLLM is a tiny api.Provider that scripts a response for Extract. Real
// hosts plug in Anthropic / OpenAI / Ollama via NewAgent's auto-detect.
type stubLLM struct{ kind api.ProviderKind }

func (p *stubLLM) Kind() api.ProviderKind { return p.kind }
func (p *stubLLM) Send(_ context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent, 2)
	errc := make(chan error, 1)
	delta, _ := json.Marshal(map[string]string{"type": "text_delta", "text": `{"name":"Ada"}`})
	go func() {
		events <- &api.StreamEvent{Type: "content_block_delta", Delta: delta}
		events <- &api.StreamEvent{Type: "message_stop"}
		close(events)
		close(errc)
	}()
	return events, errc
}

// ExampleAgent_thirdPartyEmbedding shows how a third-party Go app embeds
// ycode as its agentic backbone with the dangerous defaults stripped, an
// auth middleware in place, a domain tool that reads the caller's identity,
// one-shot structured-output extraction, and embeddings for retrieval.
//
// All five gap areas (tool sandboxing, multi-tenant auth seam, structured
// output, embeddings, actor context) are exercised below.
func ExampleAgent_thirdPartyEmbedding() {
	// (1) Build an Agent with the dangerous defaults stripped. The host
	// registers only its own domain tools afterwards.
	a, err := ycode.NewAgent(
		ycode.WithProvider(&stubLLM{kind: api.ProviderOpenAI}),
		ycode.WithoutBuiltinTools(),
		ycode.WithEmbeddingProvider(embedding.NewSimpleHashProvider(32)),
	)
	if err != nil {
		fmt.Println("err:", err)
		return
	}

	// (2) Register a domain tool that reads actor.User from ctx for authz.
	_ = a.Registry().Register(&tools.ToolSpec{
		Name:         "classgo.signoff",
		Description:  "Mark a student record signed-off.",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		RequiredMode: permission.ReadOnly,
		Handler: func(ctx context.Context, _ json.RawMessage) (string, error) {
			if !actor.HasRole(ctx, "admin") {
				return "", fmt.Errorf("forbidden")
			}
			u, _ := actor.UserFromContext(ctx)
			return "signed by " + u.ID, nil
		},
	})

	// (3) Spin up HandlerWithAuth in front of an auth middleware. The
	// middleware decodes the host's token and stamps actor.User on ctx;
	// every downstream tool handler sees it.
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Real hosts decode a header / cookie here. We synthesize.
			ctx := actor.WithUser(r.Context(), actor.User{
				ID:    "instructor-7",
				Roles: []string{"admin"},
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	srv := httptest.NewServer(a.HandlerWithAuth(mw))
	defer srv.Close()

	// Confirm the HTTP surface is reachable.
	resp, _ := http.Get(srv.URL + "/api/sessions")
	if resp != nil {
		resp.Body.Close()
	}

	// (4) Domain tool invocation under an authenticated context. In a real
	// app the LLM picks the tool name and emits JSON args via tool_use; here
	// we exercise the Registry directly to keep the example deterministic.
	ctx := actor.WithUser(context.Background(), actor.User{
		ID:    "instructor-7",
		Roles: []string{"admin"},
	})
	out, _ := a.Registry().Invoke(ctx, "classgo.signoff", json.RawMessage(`{}`))
	fmt.Println("tool:", out)

	// (5) One-shot structured output — no agent loop. Useful for
	// profile normalization, smart signoff notes, etc.
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	extracted, _ := a.Extract(context.Background(), "extract the name from: I'm Ada", ycode.ExtractOptions{
		Schema: schema,
	})
	fmt.Println("extract:", string(extracted))

	// (6) Embeddings. Same env-precedence ladder ycode uses internally.
	vec, _ := a.Embed(context.Background(), "hello")
	fmt.Println("embed dims:", len(vec))

	// Output:
	// tool: signed by instructor-7
	// extract: {"name":"Ada"}
	// embed dims: 32
}
