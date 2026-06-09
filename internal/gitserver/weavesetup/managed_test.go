package weavesetup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsLoomManaged(t *testing.T) {
	dir := t.TempDir()
	if IsLoomManaged(dir) {
		t.Errorf("fresh tempdir reports as managed")
	}
	// Drop a non-empty .ycode/loom.yaml marker.
	if err := os.MkdirAll(filepath.Join(dir, ".ycode"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ycode", "loom.yaml"), []byte("slug: x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !IsLoomManaged(dir) {
		t.Errorf("with .ycode/loom.yaml present, IsLoomManaged should be true")
	}
}

func TestIsLoomManaged_EmptyCWD(t *testing.T) {
	if IsLoomManaged("") {
		t.Errorf("empty cwd should never be managed")
	}
}

func TestIsAttached_RespectsEnv(t *testing.T) {
	t.Setenv("YCODE_LOOM_ID", "")
	if IsAttached() {
		t.Errorf("empty YCODE_LOOM_ID should report not attached")
	}
	t.Setenv("YCODE_LOOM_ID", "loom-abc")
	if !IsAttached() {
		t.Errorf("set YCODE_LOOM_ID should report attached")
	}
}
