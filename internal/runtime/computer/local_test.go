package computer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/fileops"
	"github.com/qiangli/ycode/internal/runtime/vfs"
)

func newTestComputer(t *testing.T) (*LocalComputer, string) {
	t.Helper()
	tmp := t.TempDir()
	v, err := vfs.New([]string{tmp}, nil)
	if err != nil {
		t.Fatalf("vfs.New: %v", err)
	}
	return NewLocal(v), tmp
}

func TestLocalFiles_RoundTrip(t *testing.T) {
	c, tmp := newTestComputer(t)
	ctx := context.Background()
	path := filepath.Join(tmp, "hello.txt")

	if err := c.Files().Write(ctx, fileops.WriteFileParams{Path: path, Content: "hi\n"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := c.Files().Read(ctx, fileops.ReadFileParams{Path: path})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got == "" {
		t.Fatal("Read returned empty content")
	}
}

func TestLocalFiles_RejectsOutsideAllowed(t *testing.T) {
	c, _ := newTestComputer(t)
	ctx := context.Background()
	other := t.TempDir() // separate dir, not in VFS allowlist

	err := c.Files().Write(ctx, fileops.WriteFileParams{
		Path:    filepath.Join(other, "evil.txt"),
		Content: "should not write",
	})
	if err == nil {
		t.Fatal("expected VFS to reject path outside allowed dirs")
	}
}

func TestLocalShell_Run(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	c, _ := newTestComputer(t)
	ctx := context.Background()
	res, err := c.Shell().Run(ctx, bash.ExecParams{Command: "echo hello"})
	if err != nil {
		t.Fatalf("Shell.Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
}

func TestLocalShell_SessionUnsupported(t *testing.T) {
	c, _ := newTestComputer(t)
	_, err := c.Shell().Session(context.Background(), SessionOpts{})
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("Session err = %v, want ErrNotSupported", err)
	}
}

func TestLocalWeb_Fetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()

	c, _ := newTestComputer(t)
	// Default ValidateURL rejects loopback for SSRF protection; we
	// expect this; verify the error path.
	_, err := c.Web().Fetch(context.Background(), srv.URL, FetchOpts{})
	if err == nil {
		t.Fatal("expected SSRF rejection on loopback URL")
	}
}

func TestNewLocal_AppliesOptions(t *testing.T) {
	v, _ := vfs.New([]string{t.TempDir()}, nil)
	custom := &http.Client{}
	c := NewLocal(v, WithHTTPClient(custom))
	if c.httpc != custom {
		t.Error("WithHTTPClient did not apply")
	}
}

func TestLocalFiles_StatRejectsOutside(t *testing.T) {
	c, _ := newTestComputer(t)
	other := t.TempDir()
	bogus := filepath.Join(other, "nope")
	_ = os.WriteFile(bogus, []byte("x"), 0o644)
	if _, err := c.Files().Stat(context.Background(), bogus); err == nil {
		t.Error("expected stat outside allowed to be rejected")
	}
}
