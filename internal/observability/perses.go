package observability

import (
	"fmt"
	"os"
	"path/filepath"
)

// GeneratePersesConfig produces a Perses config YAML.
func GeneratePersesConfig(prometheusPort int) string {
	return fmt.Sprintf(`database:
  file:
    folder: data
    extension: json

schemas:
  panels_path: schemas/panels
  queries_path: schemas/queries
  datasources_path: schemas/datasources
  variables_path: schemas/variables

datasources:
  - name: prometheus
    default: true
    plugin:
      kind: PrometheusDatasource
      spec:
        proxy:
          kind: HTTPProxy
          spec:
            url: http://127.0.0.1:%d
`, prometheusPort)
}

// WritePersesConfig writes perses config to the given directory.
func WritePersesConfig(dir string, prometheusPort int) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(GeneratePersesConfig(prometheusPort)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
