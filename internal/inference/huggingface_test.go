package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHFRef(t *testing.T) {
	tests := []struct {
		ref      string
		repo     string
		filename string
		wantErr  bool
	}{
		{
			ref:      "hf://bartowski/Llama-3-8B-GGUF/model-Q4_K_M.gguf",
			repo:     "bartowski/Llama-3-8B-GGUF",
			filename: "model-Q4_K_M.gguf",
		},
		{
			ref:  "hf://Qwen/Qwen2.5-7B-GGUF",
			repo: "Qwen/Qwen2.5-7B-GGUF",
		},
		{
			ref:      "bartowski/Llama-3-8B-GGUF/model.gguf",
			repo:     "bartowski/Llama-3-8B-GGUF",
			filename: "model.gguf",
		},
		{
			ref:     "hf://invalid",
			wantErr: true,
		},
		{
			ref:     "hf://",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			repo, filename, err := ParseHFRef(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo != tt.repo {
				t.Errorf("repo = %q, want %q", repo, tt.repo)
			}
			if filename != tt.filename {
				t.Errorf("filename = %q, want %q", filename, tt.filename)
			}
		})
	}
}

func TestGenerateModelfile(t *testing.T) {
	path := "/path/to/model.gguf"
	got := GenerateModelfile(path)
	want := "FROM /path/to/model.gguf\n"
	if got != want {
		t.Errorf("GenerateModelfile(%q) = %q, want %q", path, got, want)
	}
}

func TestNewHFClient_Defaults(t *testing.T) {
	t.Setenv("HF_TOKEN", "")

	hf := NewHFClient(HFConfig{})
	if hf.token != "" {
		t.Errorf("token = %q, want empty", hf.token)
	}
	if hf.cacheDir == "" {
		t.Error("cacheDir should have a default")
	}
}

func TestNewHFClient_EnvToken(t *testing.T) {
	t.Setenv("HF_TOKEN", "hf_test_token_123")

	hf := NewHFClient(HFConfig{})
	if hf.token != "hf_test_token_123" {
		t.Errorf("token = %q, want %q", hf.token, "hf_test_token_123")
	}
}

func TestNewHFClient_ExplicitConfig(t *testing.T) {
	hf := NewHFClient(HFConfig{
		Token:    "explicit-token",
		CacheDir: "/custom/cache",
	})
	if hf.token != "explicit-token" {
		t.Errorf("token = %q, want %q", hf.token, "explicit-token")
	}
	if hf.cacheDir != "/custom/cache" {
		t.Errorf("cacheDir = %q, want %q", hf.cacheDir, "/custom/cache")
	}
}

func TestHFClient_Search(t *testing.T) {
	mockModels := []HFModel{
		{ID: "bartowski/Llama-3-8B-GGUF", Downloads: 50000, Likes: 100},
		{ID: "TheBloke/Mistral-7B-GGUF", Downloads: 30000, Likes: 80},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		// Verify query params.
		q := r.URL.Query()
		if q.Get("filter") != "gguf" {
			t.Errorf("filter = %q, want %q", q.Get("filter"), "gguf")
		}
		if q.Get("search") != "llama" {
			t.Errorf("search = %q, want %q", q.Get("search"), "llama")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockModels)
	}))
	defer srv.Close()

	hf := &HFClient{
		client:   srv.Client(),
		cacheDir: t.TempDir(),
	}
	// Override the search URL by replacing the base in the client.
	// Since Search() constructs the URL, we need to patch it.
	// Instead, test with a custom transport.
	hf.client = &http.Client{
		Transport: &rewriteTransport{base: srv.URL, inner: http.DefaultTransport},
	}

	models, err := hf.Search(context.Background(), "llama", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].ID != "bartowski/Llama-3-8B-GGUF" {
		t.Errorf("models[0].ID = %q", models[0].ID)
	}
}

// rewriteTransport redirects all requests to a local test server.
type rewriteTransport struct {
	base  string
	inner http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.base[len("http://"):]
	return t.inner.RoundTrip(req)
}

func TestHFClient_Search_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	hf := &HFClient{
		client:   &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: http.DefaultTransport}},
		cacheDir: t.TempDir(),
	}

	_, err := hf.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestHFClient_DownloadGGUF(t *testing.T) {
	content := "fake-gguf-model-data-for-testing"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	hf := &HFClient{
		client:   &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: http.DefaultTransport}},
		cacheDir: cacheDir,
	}

	var progressCalled bool
	path, err := hf.DownloadGGUF(context.Background(), "test/repo", "model.gguf", func(downloaded, total int64) {
		progressCalled = true
	})
	if err != nil {
		t.Fatalf("DownloadGGUF: %v", err)
	}

	if !progressCalled {
		t.Error("progress callback was not called")
	}

	// Verify file exists and has content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content = %q, want %q", string(data), content)
	}

	// Verify path structure.
	expectedDir := filepath.Join(cacheDir, "test--repo")
	if filepath.Dir(path) != expectedDir {
		t.Errorf("file dir = %q, want %q", filepath.Dir(path), expectedDir)
	}
}

func TestHFClient_DownloadGGUF_Cached(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Write([]byte("model-data"))
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	hf := &HFClient{
		client:   &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: http.DefaultTransport}},
		cacheDir: cacheDir,
	}

	// First download.
	_, err := hf.DownloadGGUF(context.Background(), "test/repo", "model.gguf", nil)
	if err != nil {
		t.Fatalf("first download: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected 1 request, got %d", requestCount)
	}

	// Second download — should be cached.
	_, err = hf.DownloadGGUF(context.Background(), "test/repo", "model.gguf", nil)
	if err != nil {
		t.Fatalf("second download: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("expected 1 request (cached), got %d", requestCount)
	}
}

func TestHFClient_DownloadGGUF_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	hf := &HFClient{
		client:   &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: http.DefaultTransport}},
		cacheDir: t.TempDir(),
	}

	_, err := hf.DownloadGGUF(context.Background(), "test/repo", "missing.gguf", nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestHFClient_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]HFModel{})
	}))
	defer srv.Close()

	hf := &HFClient{
		token:    "hf_secret_token",
		client:   &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: http.DefaultTransport}},
		cacheDir: t.TempDir(),
	}

	hf.Search(context.Background(), "test", 5)

	if gotAuth != "Bearer hf_secret_token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer hf_secret_token")
	}
}
