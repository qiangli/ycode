//go:build release_smoke && embed_runner

// Release smoke test: exercises the inference subsystem end-to-end on
// the bare-minimum use case (pull model + single-token chat) so a
// broken or mis-configured embedded inference runner fails CI before
// it lands in a release artifact.
//
// Run with:
//
//	make test-release-smoke
//
// The `embed_runner` build constraint is mandatory — without it,
// runnerEmbed.Available() returns false and the inference component
// hard-stops at startup (see internal/inference/ollama.go). Pulling
// this file behind the same tag matches that contract: the test only
// builds when the binary it's testing actually includes the runner.
//
// Model: defaults to qwen2.5:0.5b (~400 MB, has chat template, known
// to work end-to-end on this stack). Override via OLLAMA_TEST_MODEL
// for a smaller candidate. First-time pull is bandwidth-bound; the
// blob cache makes subsequent runs ~seconds.

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/coreutils/external/ollama"
)

// TestReleaseSmoke_OllamaPullAndChat starts the in-process ollama
// server, pulls a tiny model, and verifies a chat completion returns
// non-empty content. Total wall-clock: ~10 s on a warm cache, several
// minutes on a cold one.
func TestReleaseSmoke_OllamaPullAndChat(t *testing.T) {
	model := os.Getenv("OLLAMA_TEST_MODEL")
	if model == "" {
		model = "qwen2.5:0.5b"
	}

	dataDir := filepath.Join(t.TempDir(), "inference")
	comp := ollama.NewOllamaComponent(&ollama.Config{}, dataDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("OllamaComponent.Start: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = comp.Stop(stopCtx)
	}()

	base := comp.BaseURL()
	if base == "" {
		t.Fatal("BaseURL() empty after Start")
	}

	// 1. Pull. /api/pull is a streaming endpoint emitting one JSON
	//    object per line; we drain until we see a terminal "success"
	//    status or hit the deadline.
	if err := ollamaPull(ctx, t, base, model); err != nil {
		t.Fatalf("pull %s: %v", model, err)
	}

	// 2. Chat. stream:false so we get a single JSON envelope back.
	reply, err := ollamaChatOnce(ctx, base, model, "reply with one word: hi")
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if strings.TrimSpace(reply) == "" {
		t.Fatalf("empty model reply")
	}
	t.Logf("model reply: %q", strings.TrimSpace(reply))
}

func ollamaPull(ctx context.Context, t *testing.T, base, model string) error {
	body, _ := json.Marshal(map[string]any{"model": model})
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		return errFromStatus(resp.StatusCode, out)
	}
	dec := json.NewDecoder(resp.Body)
	for {
		var evt struct {
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		}
		if err := dec.Decode(&evt); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if evt.Error != "" {
			return errFromStatus(0, []byte(evt.Error))
		}
		// `success` is ollama's terminal status for a finished pull.
		if evt.Status == "success" {
			t.Logf("pull complete: %s", model)
			return nil
		}
	}
	return nil
}

func ollamaChatOnce(ctx context.Context, base, model, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":  model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"options": map[string]any{"num_predict": 8},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", errFromStatus(resp.StatusCode, out)
	}
	var env struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return "", err
	}
	return env.Message.Content, nil
}

func errFromStatus(code int, body []byte) error {
	return &smokeErr{code: code, body: string(body)}
}

type smokeErr struct {
	code int
	body string
}

func (e *smokeErr) Error() string {
	if e.code == 0 {
		return e.body
	}
	return "HTTP " + http.StatusText(e.code) + ": " + e.body
}
