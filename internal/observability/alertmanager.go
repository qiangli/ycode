package observability

import (
	"os"
	"path/filepath"
)

// GenerateAlertmanagerConfig produces a minimal alertmanager.yml.
func GenerateAlertmanagerConfig() string {
	return `global:
  resolve_timeout: 5m

route:
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'default'

receivers:
  - name: 'default'
`
}

// WriteAlertmanagerConfig writes alertmanager.yml to the given directory.
func WriteAlertmanagerConfig(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(GenerateAlertmanagerConfig()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
