package inference

import (
	"testing"

	"github.com/qiangli/ycode/internal/api"
)

func TestLocalFallbackConfig(t *testing.T) {
	cfg := LocalFallbackConfig("http://127.0.0.1:11434")

	if cfg.Kind != api.ProviderLocal {
		t.Errorf("Kind = %q, want %q", cfg.Kind, api.ProviderLocal)
	}
	if cfg.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "http://127.0.0.1:11434/v1")
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", cfg.APIKey)
	}
}

func TestNewLocalProvider_NilComponent(t *testing.T) {
	_, err := NewLocalProvider(nil)
	if err == nil {
		t.Fatal("expected error for nil component")
	}
}

func TestNewLocalProvider_UnhealthyComponent(t *testing.T) {
	comp := NewOllamaComponent(&Config{Enabled: true}, t.TempDir())
	// Component is not started, so not healthy.
	_, err := NewLocalProvider(comp)
	if err == nil {
		t.Fatal("expected error for unhealthy component")
	}
}
