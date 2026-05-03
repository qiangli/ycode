package inference

import (
	"context"
	"time"

	"github.com/qiangli/ycode/internal/api"
)

// NewOllamaLister returns an api.OllamaLister that queries the default Ollama
// server for locally available models. The returned function uses a 2-second
// timeout for server detection and returns nil if Ollama is unreachable.
func NewOllamaLister() api.OllamaLister {
	return func(ctx context.Context) []api.ModelInfo {
		baseURL := DefaultOllamaURL()

		detectCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if !DetectOllamaServer(detectCtx, baseURL) {
			return nil
		}

		ollamaModels, err := OllamaListModels(ctx, baseURL)
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
