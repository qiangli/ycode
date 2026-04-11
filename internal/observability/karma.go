package observability

import (
	"fmt"
	"os"
	"path/filepath"
)

// GenerateKarmaConfig produces a karma config YAML.
func GenerateKarmaConfig(alertmanagerPort int) string {
	return fmt.Sprintf(`alertmanager:
  servers:
    - name: local
      uri: http://127.0.0.1:%d
`, alertmanagerPort)
}

// WriteKarmaConfig writes karma.yaml to the given directory.
func WriteKarmaConfig(dir string, alertmanagerPort int) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(GenerateKarmaConfig(alertmanagerPort)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
