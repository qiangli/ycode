package inference

import (
	"context"
	"time"

	"github.com/qiangli/ycode/internal/api"
)

// NewOllamaLister returns an api.OllamaLister that queries the default Ollama
// server for locally available models. The returned function uses a 2-second
// timeout for server detection and a 3-second timeout for the model list,
// returning nil if Ollama is unreachable or slow. The list-call timeout is
// load-bearing: it runs on the TUI Update goroutine via the /model picker
// path, so any blocking call here freezes the screen.
func NewOllamaLister() api.OllamaLister {
	return func(ctx context.Context) []api.ModelInfo {
		baseURL := DefaultOllamaURL()

		detectCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if !DetectOllamaServer(detectCtx, baseURL) {
			return nil
		}

		listCtx, cancelList := context.WithTimeout(ctx, 3*time.Second)
		defer cancelList()

		ollamaModels, err := OllamaListModels(listCtx, baseURL)
		if err != nil {
			return nil
		}

		var models []api.ModelInfo
		for _, m := range ollamaModels {
			models = append(models, api.ModelInfo{
				ID:       m.Name,
				Provider: "ollama",
				Source:   "ollama",
				Size:     api.FormatBytes(m.Size),
			})
		}
		return models
	}
}
