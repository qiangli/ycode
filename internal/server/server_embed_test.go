package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qiangli/ycode/internal/bus"
)

// stubEmbedder is a deterministic embedding provider for tests. It emits a
// fixed-dimension zero-vector regardless of input — enough to verify the
// wire shape of /api/embed* without requiring a real embedder.
type stubEmbedder struct {
	dims int
}

func (s *stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, s.dims)
	for i := range v {
		v[i] = 0.01 * float32(i)
	}
	return v, nil
}

func (s *stubEmbedder) Dimensions() int { return s.dims }

func newEmbedTestServer(t *testing.T, dims int) (*Server, *httptest.Server) {
	t.Helper()
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })

	svc := &mockService{b: memBus}
	srv := New(Config{}, svc)
	srv.setEmbedProviderForTest(&stubEmbedder{dims: dims})

	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestHandleEmbed_OK(t *testing.T) {
	_, ts := newEmbedTestServer(t, 8)

	body := embedRequest{Text: "hello"}
	buf, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+"/api/embed", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	var got embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Dimensions != 8 {
		t.Errorf("dims = %d, want 8", got.Dimensions)
	}
	if len(got.Vector) != 8 {
		t.Errorf("vector len = %d, want 8", len(got.Vector))
	}
}

func TestHandleEmbed_RequiresText(t *testing.T) {
	_, ts := newEmbedTestServer(t, 8)

	resp, err := http.Post(ts.URL+"/api/embed", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("got %d, want 400", resp.StatusCode)
	}
}

func TestHandleEmbedBatch_OK(t *testing.T) {
	_, ts := newEmbedTestServer(t, 4)

	body := embedBatchRequest{Texts: []string{"a", "b", "c"}}
	buf, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+"/api/embed/batch", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	var got embedBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Vectors) != 3 {
		t.Errorf("got %d vectors, want 3", len(got.Vectors))
	}
	if got.Dimensions != 4 {
		t.Errorf("dims = %d, want 4", got.Dimensions)
	}
}

func TestHandleEmbedDimensions_OK(t *testing.T) {
	_, ts := newEmbedTestServer(t, 32)

	resp, err := http.Get(ts.URL + "/api/embed/dimensions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	var got embedDimensionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Dimensions != 32 {
		t.Errorf("dims = %d, want 32", got.Dimensions)
	}
}
