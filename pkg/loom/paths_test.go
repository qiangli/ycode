package loom

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultLeasePath(t *testing.T) {
	got := DefaultLeasePath("/tmp/gitea")
	want := filepath.Join("/tmp", "gitea", "loom", "leases.json")
	if got != want {
		t.Errorf("DefaultLeasePath: got %q want %q", got, want)
	}
}

func TestDefaultSandboxRoot(t *testing.T) {
	got := DefaultSandboxRoot("/tmp/gitea")
	want := filepath.Join("/tmp", "gitea", "loom", "sandboxes")
	if got != want {
		t.Errorf("DefaultSandboxRoot: got %q want %q", got, want)
	}
}

func TestDefaultGiteaDataDir(t *testing.T) {
	got, err := DefaultGiteaDataDir()
	if err != nil {
		t.Fatalf("DefaultGiteaDataDir: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".agents", "ycode", "observability", "gitea")) {
		t.Errorf("DefaultGiteaDataDir: %q does not end with .agents/ycode/observability/gitea", got)
	}
}

func TestDefaultGiteaDataDirWithEnv(t *testing.T) {
	t.Setenv("YCODE_GITEA_DATA_DIR", "/custom/gitea/path")
	got, err := DefaultGiteaDataDirWithEnv()
	if err != nil {
		t.Fatalf("DefaultGiteaDataDirWithEnv: %v", err)
	}
	if got != "/custom/gitea/path" {
		t.Errorf("DefaultGiteaDataDirWithEnv: env override not honored — got %q want %q", got, "/custom/gitea/path")
	}

	// Unset: fall back to default.
	t.Setenv("YCODE_GITEA_DATA_DIR", "")
	got2, err := DefaultGiteaDataDirWithEnv()
	if err != nil {
		t.Fatalf("DefaultGiteaDataDirWithEnv fallback: %v", err)
	}
	if !strings.HasSuffix(got2, filepath.Join(".agents", "ycode", "observability", "gitea")) {
		t.Errorf("DefaultGiteaDataDirWithEnv fallback: got %q", got2)
	}
}
