package collector

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

const (
	// DefaultVersion is the default OTEL Collector Contrib version.
	DefaultVersion = "0.115.0"
	// releaseURLTemplate is the GitHub release download URL.
	releaseURLTemplate = "https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v%s/otelcol-contrib_%s_%s_%s%s"
)

// EnsureBinary downloads the OTEL Collector binary if not already present.
// Returns the path to the binary.
func EnsureBinary(ctx context.Context, binDir, version string) (string, error) {
	if version == "" {
		version = DefaultVersion
	}
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}

	name := fmt.Sprintf("otelcol-contrib-%s-%s-%s%s", version, goos, goarch, ext)
	path := filepath.Join(binDir, name)

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create bin dir: %w", err)
	}

	url := fmt.Sprintf(releaseURLTemplate, version, version, goos, goarch, ext)
	return path, downloadFile(ctx, url, path)
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
