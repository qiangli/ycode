package bash

import (
	"context"
	"errors"
	"testing"
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
