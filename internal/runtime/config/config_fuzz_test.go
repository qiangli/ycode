package config

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzConfigLoading(f *testing.F) {
	f.Add(`{"model": "test"}`)
	f.Add(`{"maxTokens": 1000}`)
	f.Add(`{}`)
	f.Add(`{"custom": {"key": "value"}}`)
	f.Add(`invalid json`)

	f.Fuzz(func(t *testing.T, input string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
			return
		}

		cfg := DefaultConfig()
		_ = mergeFromFile(cfg, path)
	})
}
