// Package vfkit_embed provides a self-extracting embedded vfkit binary.
// vfkit is the Apple Virtualization Framework helper that podman machine
// uses to run Linux VMs on macOS. It's ~5MB compressed to ~2MB.
//
// Build with: make vfkit-embed
package vfkit_embed

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const binaryName = "vfkit"

// compressedVfkit holds the gzip-compressed vfkit binary.
var compressedVfkit []byte

// Available returns true if an embedded vfkit binary is present.
func Available() bool {
	return len(compressedVfkit) > 0
}

// EnsureVfkit extracts the embedded vfkit binary to cacheDir if not already
// present or if the embedded version has changed.
func EnsureVfkit(cacheDir string) (string, error) {
	if !Available() {
		return "", fmt.Errorf("no embedded vfkit (build with: make vfkit-embed)")
	}

	binPath := filepath.Join(cacheDir, binaryName)
	hashPath := binPath + ".sha256"

	h := sha256.Sum256(compressedVfkit)
	embeddedHash := fmt.Sprintf("%x", h)

	if cachedHash, err := os.ReadFile(hashPath); err == nil {
		if string(cachedHash) == embeddedHash {
			if info, err := os.Stat(binPath); err == nil && info.Mode().IsRegular() {
				return binPath, nil
			}
		}
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	gr, err := gzip.NewReader(bytes.NewReader(compressedVfkit))
	if err != nil {
		return "", fmt.Errorf("decompress vfkit: %w", err)
	}
	defer gr.Close()

	tmpPath := binPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", fmt.Errorf("create vfkit binary: %w", err)
	}
	if _, err := io.Copy(f, gr); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write vfkit binary: %w", err)
	}
	f.Close()

	if err := os.Rename(tmpPath, binPath); err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	if runtime.GOOS == "darwin" {
		exec.Command("codesign", "-f", "-s", "-", binPath).Run() //nolint:errcheck
	}

	os.WriteFile(hashPath, []byte(embeddedHash), 0o644) //nolint:errcheck
	return binPath, nil
}
