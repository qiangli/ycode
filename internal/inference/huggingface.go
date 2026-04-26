package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	ollamaembed "github.com/ollama/ollama/embed"
)

// HFModel represents a model from the Hugging Face Hub API.
type HFModel struct {
	ID        string   `json:"id"`        // e.g. "bartowski/Llama-3-8B-GGUF"
	Tags      []string `json:"tags"`      // e.g. ["gguf", "llama"]
	Downloads int      `json:"downloads"` // download count
	Likes     int      `json:"likes"`
}

// HFClient provides access to the Hugging Face Hub for GGUF model discovery
// and download.
type HFClient struct {
	token    string // HF API token (for gated models)
	cacheDir string // download cache directory
	client   *http.Client
}

// NewHFClient creates a Hugging Face Hub client.
// Token falls back to $HF_TOKEN environment variable.
func NewHFClient(cfg HFConfig) *HFClient {
	token := cfg.Token
	if token == "" {
		token = os.Getenv("HF_TOKEN")
	}
	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache", "huggingface", "hub")
	}
	return &HFClient{
		token:    token,
		cacheDir: cacheDir,
		client:   &http.Client{},
	}
}

// Search queries the Hugging Face Hub for GGUF models matching the query.
func (c *HFClient) Search(ctx context.Context, query string, limit int) ([]HFModel, error) {
	if limit <= 0 {
		limit = 20
	}
	url := fmt.Sprintf("https://huggingface.co/api/models?filter=gguf&search=%s&sort=downloads&direction=-1&limit=%d",
		query, limit)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hf search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("hf search: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var models []HFModel
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("hf search: decode: %w", err)
	}
	return models, nil
}

// DownloadGGUF downloads a GGUF file from a Hugging Face repo.
// Returns the local file path.
func (c *HFClient) DownloadGGUF(ctx context.Context, repo, filename string, progress func(downloaded, total int64)) (string, error) {
	os.MkdirAll(c.cacheDir, 0o755)

	// Sanitize repo name for filesystem.
	safeRepo := strings.ReplaceAll(repo, "/", "--")
	localPath := filepath.Join(c.cacheDir, safeRepo, filename)

	// Skip if already downloaded.
	if info, err := os.Stat(localPath); err == nil && info.Size() > 0 {
		slog.Info("hf: model already cached", "path", localPath)
		return localPath, nil
	}

	os.MkdirAll(filepath.Dir(localPath), 0o755)

	url := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repo, filename)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("hf download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("hf download: HTTP %d for %s", resp.StatusCode, url)
	}

	total := resp.ContentLength

	f, err := os.Create(localPath + ".tmp")
	if err != nil {
		return "", fmt.Errorf("hf download: create file: %w", err)
	}
	defer f.Close()

	var downloaded int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				os.Remove(localPath + ".tmp")
				return "", fmt.Errorf("hf download: write: %w", writeErr)
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			os.Remove(localPath + ".tmp")
			return "", fmt.Errorf("hf download: read: %w", readErr)
		}
	}
	f.Close()

	if err := os.Rename(localPath+".tmp", localPath); err != nil {
		return "", fmt.Errorf("hf download: rename: %w", err)
	}

	slog.Info("hf: downloaded", "repo", repo, "file", filename, "size", downloaded)
	return localPath, nil
}

// GenerateModelfile creates an Ollama Modelfile for a downloaded GGUF.
// This allows importing the model into Ollama's local registry.
func GenerateModelfile(ggufPath string) string {
	return fmt.Sprintf("FROM %s\n", ggufPath)
}

// ParseHFRef parses a "hf://owner/repo/filename.gguf" or "hf://owner/repo" reference.
// Returns (repo, filename). If filename is empty, the caller should list GGUF files.
func ParseHFRef(ref string) (repo, filename string, err error) {
	ref = strings.TrimPrefix(ref, "hf://")
	parts := strings.SplitN(ref, "/", 3)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid HF reference %q: expected hf://owner/repo[/file.gguf]", ref)
	}
	repo = parts[0] + "/" + parts[1]
	if len(parts) == 3 {
		filename = parts[2]
	}
	return repo, filename, nil
}

// ImportGGUFToOllama imports a downloaded GGUF file into Ollama's local registry.
// The model becomes immediately runnable after this call returns.
func ImportGGUFToOllama(ctx context.Context, ollamaBaseURL, modelName, ggufPath string, progress func(status string)) error {
	client, err := ollamaembed.NewClient(ollamaBaseURL)
	if err != nil {
		return fmt.Errorf("create ollama client: %w", err)
	}

	req := &ollamaembed.CreateRequest{
		Model: modelName,
		From:  ggufPath,
	}

	return client.Create(ctx, req, func(resp ollamaembed.ProgressResponse) error {
		if progress != nil && resp.Status != "" {
			progress(resp.Status)
		}
		return nil
	})
}

// DeriveModelName generates an Ollama-friendly model name from a HF reference.
// Example: "bartowski/Llama-3-8B-GGUF", "Llama-3-8B-Q4_K_M.gguf" → "llama-3-8b-q4-k-m"
func DeriveModelName(repo, filename string) string {
	// Strip extension.
	name := strings.TrimSuffix(filename, ".gguf")
	name = strings.TrimSuffix(name, ".GGUF")

	// Lowercase.
	name = strings.ToLower(name)

	// Replace underscores with hyphens.
	name = strings.ReplaceAll(name, "_", "-")

	// Collapse repeated hyphens and strip non-alphanumeric (except hyphen/dot/colon).
	name = regexp.MustCompile(`[^a-z0-9.\-:]`).ReplaceAllString(name, "-")
	name = regexp.MustCompile(`-{2,}`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")

	if name == "" {
		// Fallback to repo name.
		parts := strings.Split(repo, "/")
		name = strings.ToLower(parts[len(parts)-1])
	}

	return name
}

// DetectOllamaServer checks if an Ollama server is reachable at the given URL.
func DetectOllamaServer(ctx context.Context, baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// DefaultOllamaURL returns the default Ollama server URL.
func DefaultOllamaURL() string {
	if u := os.Getenv("OLLAMA_HOST"); u != "" {
		if !strings.HasPrefix(u, "http") {
			return "http://" + u
		}
		return u
	}
	return "http://127.0.0.1:11434"
}

// OllamaListModels lists models from a running Ollama server.
func OllamaListModels(ctx context.Context, baseURL string) ([]ollamaembed.ListResponse, error) {
	client, err := ollamaembed.NewClient(baseURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.List(ctx)
	if err != nil {
		return nil, err
	}
	// Wrap in slice for type compatibility — caller uses resp.Models directly.
	return []ollamaembed.ListResponse{*resp}, nil
}

// OllamaPullModel pulls a model from the Ollama registry.
func OllamaPullModel(ctx context.Context, baseURL, modelName string, progress func(status string, completed, total int64)) error {
	client, err := ollamaembed.NewClient(baseURL)
	if err != nil {
		return err
	}
	req := &ollamaembed.PullRequest{Model: modelName}
	return client.Pull(ctx, req, func(resp ollamaembed.ProgressResponse) error {
		if progress != nil {
			progress(resp.Status, resp.Completed, resp.Total)
		}
		return nil
	})
}

// OllamaDeleteModel deletes a model from the Ollama server.
func OllamaDeleteModel(ctx context.Context, baseURL, modelName string) error {
	client, err := ollamaembed.NewClient(baseURL)
	if err != nil {
		return err
	}
	return client.Delete(ctx, &ollamaembed.DeleteRequest{Model: modelName})
}
