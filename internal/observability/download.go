package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// BinarySpec describes a downloadable binary.
type BinarySpec struct {
	Name    string
	Version string
	// URLFunc returns the download URL for the given OS and architecture.
	URLFunc func(goos, goarch string) string
}

// DefaultBinaries returns the binary specs for all stack components.
func DefaultBinaries() []BinarySpec {
	return []BinarySpec{
		{
			Name:    "prometheus",
			Version: "3.4.0",
			URLFunc: func(goos, goarch string) string {
				return fmt.Sprintf(
					"https://github.com/prometheus/prometheus/releases/download/v3.4.0/prometheus-3.4.0.%s-%s.tar.gz",
					goos, goarch)
			},
		},
		{
			Name:    "alertmanager",
			Version: "0.28.1",
			URLFunc: func(goos, goarch string) string {
				return fmt.Sprintf(
					"https://github.com/prometheus/alertmanager/releases/download/v0.28.1/alertmanager-0.28.1.%s-%s.tar.gz",
					goos, goarch)
			},
		},
		{
			Name:    "karma",
			Version: "0.120",
			URLFunc: func(goos, goarch string) string {
				return fmt.Sprintf(
					"https://github.com/prymitive/karma/releases/download/v0.120/karma-%s-%s",
					goos, goarch)
			},
		},
		{
			Name:    "perses",
			Version: "0.50.1",
			URLFunc: func(goos, goarch string) string {
				return fmt.Sprintf(
					"https://github.com/perses/perses/releases/download/v0.50.1/perses_%s_%s.tar.gz",
					goos, goarch)
			},
		},
		{
			Name:    "victoria-logs",
			Version: "1.18.0",
			URLFunc: func(goos, goarch string) string {
				return fmt.Sprintf(
					"https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/v1.18.0-victorialogs/victoria-logs-%s-%s-v1.18.0-victorialogs.tar.gz",
					goos, goarch)
			},
		},
	}
}

// EnsureBinary downloads a binary if not present. Returns the path.
func EnsureBinary(ctx context.Context, binDir string, spec BinarySpec) (string, error) {
	name := fmt.Sprintf("%s-%s-%s-%s", spec.Name, spec.Version, runtime.GOOS, runtime.GOARCH)
	path := filepath.Join(binDir, name)

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create bin dir: %w", err)
	}

	url := spec.URLFunc(runtime.GOOS, runtime.GOARCH)
	if err := downloadFile(ctx, url, path); err != nil {
		return "", fmt.Errorf("download %s: %w", spec.Name, err)
	}

	// Make executable.
	_ = os.Chmod(path, 0o755)

	return path, nil
}

// EnsureAllBinaries downloads all stack component binaries.
func EnsureAllBinaries(ctx context.Context, binDir string) (map[string]string, error) {
	paths := make(map[string]string)
	for _, spec := range DefaultBinaries() {
		path, err := EnsureBinary(ctx, binDir, spec)
		if err != nil {
			return paths, err
		}
		paths[spec.Name] = path
	}
	return paths, nil
}

// VersionManifest returns the versions of all managed binaries.
func VersionManifest() map[string]string {
	versions := make(map[string]string)
	for _, spec := range DefaultBinaries() {
		versions[spec.Name] = spec.Version
	}
	return versions
}

// WriteVersionManifest writes the version manifest to binDir/versions.json.
func WriteVersionManifest(binDir string) error {
	data, err := json.MarshalIndent(VersionManifest(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(binDir, "versions.json"), data, 0o644)
}

func downloadFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	f.Close()

	return os.Rename(tmp, dest)
}
