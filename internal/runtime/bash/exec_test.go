package bash

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNeedsTTY(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		// Interactive commands.
		{"ssh user@host", true},
		{"scp file.txt user@host:/tmp/", true},
		{"sftp user@host", true},
		{"sudo apt install vim", true},
		{"su - root", true},
		{"passwd", true},
		{"mysql -u root -p", true},
		{"psql -U postgres", true},
		{"vim file.go", true},
		{"nano README.md", true},
		{"less output.log", true},
		{"gcloud auth login", true},
		{"docker login", true},
		{"gh auth login", true},
		{"ftp ftp.example.com", true},

		// Pipes to interactive pagers.
		{"cat file | less", true},
		{"grep pattern file | more", true},

		// SSH with BatchMode=yes is non-interactive (connectivity testing).
		{"ssh -o BatchMode=yes -o ConnectTimeout=5 user@host echo ok", false},
		{"ssh -o BatchMode=yes user@host exit", false},
		{"scp -o BatchMode=yes file.txt user@host:/tmp/", false},

		// Non-interactive commands.
		{"ls -la", false},
		{"cat file.txt", false},
		{"grep pattern file", false},
		{"git status", false},
		{"go build ./...", false},
		{"echo hello", false},
		{"curl -s https://example.com", false},
		{"make build", false},
		{"docker ps", false},
		{"gcloud compute instances list", false},
		{"npm install", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := NeedsTTY(tt.command); got != tt.want {
			t.Errorf("NeedsTTY(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}

func TestExecuteRejectsInteractive(t *testing.T) {
	_, err := Execute(context.Background(), ExecParams{Command: "ssh user@host"})
	if err == nil {
		t.Fatal("expected error for interactive command")
	}
	if !errors.Is(err, ErrNeedsTTY) {
		t.Errorf("expected ErrNeedsTTY, got: %v", err)
	}
}

func TestExecuteDelegatesToTTYExecutor(t *testing.T) {
	called := false
	ttyExec := &mockTTYExecutor{
		fn: func(ctx context.Context, command, workDir string) (*ExecResult, error) {
			called = true
			if command != "ssh user@host" {
				t.Errorf("unexpected command: %s", command)
			}
			if workDir != "/tmp" {
				t.Errorf("unexpected workDir: %s", workDir)
			}
			return &ExecResult{Stdout: "connected", ExitCode: 0}, nil
		},
	}

	result, err := Execute(context.Background(), ExecParams{
		Command: "ssh user@host",
		WorkDir: "/tmp",
		TTYExec: ttyExec,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("TTY executor was not called")
	}
	if result.Stdout != "connected" {
		t.Errorf("unexpected stdout: %q", result.Stdout)
	}
}

func TestExecuteNoTTYExecutorRejectsInteractive(t *testing.T) {
	// Without TTY executor, interactive commands should still be rejected.
	_, err := Execute(context.Background(), ExecParams{Command: "ssh user@host"})
	if err == nil {
		t.Fatal("expected error for interactive command without TTY executor")
	}
	if !errors.Is(err, ErrNeedsTTY) {
		t.Errorf("expected ErrNeedsTTY, got: %v", err)
	}
}

type mockTTYExecutor struct {
	fn func(ctx context.Context, command, workDir string) (*ExecResult, error)
}

func (m *mockTTYExecutor) ExecuteTTY(ctx context.Context, command, workDir string) (*ExecResult, error) {
	return m.fn(ctx, command, workDir)
}

func TestExecuteDevNullStdin(t *testing.T) {
	// A command that reads stdin should get EOF, not hang.
	result, err := Execute(context.Background(), ExecParams{
		Command: "cat -",
		Timeout: 3000, // 3 second timeout
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// cat - with /dev/null stdin should produce empty output.
	if result.Stdout != "" {
		t.Errorf("expected empty stdout, got %q", result.Stdout)
	}
}

func TestExecuteWithStdin(t *testing.T) {
	result, err := Execute(context.Background(), ExecParams{
		Command: "cat -",
		Stdin:   "hello from stdin",
		Timeout: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stdout != "hello from stdin" {
		t.Errorf("expected 'hello from stdin', got %q", result.Stdout)
	}
}

func TestExecuteWithStdinMultiline(t *testing.T) {
	result, err := Execute(context.Background(), ExecParams{
		Command: "wc -l",
		Stdin:   "line1\nline2\nline3\n",
		Timeout: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// wc -l should output "3" (possibly with whitespace).
	if result.Stdout != "3" && result.Stdout != "       3" {
		t.Errorf("expected line count 3, got %q", result.Stdout)
	}
}

func TestCommandTimeoutHint(t *testing.T) {
	tests := []struct {
		command string
		want    time.Duration
	}{
		// Quick commands: 30s.
		{"ls -la", 30 * time.Second},
		{"cat file.txt", 30 * time.Second},
		{"echo hello", 30 * time.Second},
		{"pwd", 30 * time.Second},
		{"which go", 30 * time.Second},

		// Build commands: 5 minutes.
		{"make build", 5 * time.Minute},
		{"go build ./...", 5 * time.Minute},
		{"npm install", 5 * time.Minute},
		{"cargo build", 5 * time.Minute},
		{"pip install requests", 5 * time.Minute},
		{"gcc -o main main.c", 5 * time.Minute},

		// Heavy operations: max timeout (10 min).
		{"docker build -t myimage .", MaxTimeout},
		{"podman build -t myimage .", MaxTimeout},

		// Download commands: 5 minutes.
		{"wget https://example.com/file.tar.gz", 5 * time.Minute},
		{"curl -O https://example.com/file.tar.gz", 5 * time.Minute},

		// Default: standard timeout.
		{"git status", DefaultTimeout},
		{"some-custom-tool --flag", DefaultTimeout},
		{"", DefaultTimeout},

		// Docker without build/pull: standard.
		{"docker ps", DefaultTimeout},
	}

	for _, tt := range tests {
		got := CommandTimeoutHint(tt.command)
		if got != tt.want {
			t.Errorf("CommandTimeoutHint(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}

func TestExecuteBackgroundWithJobRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	jobs := NewJobRegistry()
	result, err := Execute(context.Background(), ExecParams{
		Command:    "echo background_output",
		Background: true,
		Jobs:       jobs,
		WorkDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stdout == "" {
		t.Fatal("expected job start message")
	}

	// Should have one job in registry.
	jobList := jobs.List()
	if len(jobList) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobList))
	}
}

func TestExecuteBackgroundWithoutRegistry(t *testing.T) {
	// Without a job registry, background execution should fall through
	// to synchronous execution.
	result, err := Execute(context.Background(), ExecParams{
		Command:    "echo sync_fallback",
		Background: true,
		Jobs:       nil,
		WorkDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stdout != "sync_fallback" {
		t.Errorf("expected 'sync_fallback', got %q", result.Stdout)
	}
}
