package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/pkg/memex/memory"
)

func TestAwaitPromptContextDeadlineKeepsCompletedEnrichment(t *testing.T) {
	filesCh := make(chan []prompt.ContextFile, 1)
	filesCh <- []prompt.ContextFile{{Path: "AGENTS.md", Content: "instructions"}}
	close(filesCh)

	// These model a stalled repository scanner and memory backend.
	memoriesCh := make(chan []*memory.Memory)
	repoMapCh := make(chan string)
	ctx := &prompt.ProjectContext{}

	started := time.Now()
	startupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	awaitPromptContext(startupCtx, ctx, filesCh, memoriesCh, repoMapCh)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("context startup exceeded its deadline: %v", elapsed)
	}
	if len(ctx.ContextFiles) != 1 || ctx.ContextFiles[0].Path != "AGENTS.md" {
		t.Fatalf("completed context files were discarded: %#v", ctx.ContextFiles)
	}
	if ctx.RepoMapText != "" {
		t.Fatalf("stalled repo map unexpectedly populated context: %q", ctx.RepoMapText)
	}
}

func TestStartupRepoMapHelperIsKilledAtDeadline(t *testing.T) {
	if os.Getenv("YCODE_TEST_STALLED_STARTUP_HELPER") == "1" {
		time.Sleep(time.Minute)
		return
	}
	t.Setenv("YCODE_TEST_STALLED_STARTUP_HELPER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runStartupRepoMapHelper(ctx, os.Args[0], "-test.run=TestStartupRepoMapHelperIsKilledAtDeadline")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("helper error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("helper process survived deadline: %v", elapsed)
	}
}

func TestStartupRepoMapDoesNotDescendSubmoduleWorktrees(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package root\nfunc RootSymbol() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("function JavascriptRootSymbol() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "child.go"), []byte("package child\nfunc SubmoduleSymbolMustNotLoad() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitmodules"), []byte("[submodule \"child\"]\npath = child\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := generateStartupRepoMap(root)
	if err != nil {
		t.Fatalf("startupRepoMap: %v", err)
	}
	if !strings.Contains(got, "RootSymbol") {
		t.Fatalf("repo map omitted root source: %q", got)
	}
	if !strings.Contains(got, "JavascriptRootSymbol") {
		t.Fatalf("repo map omitted ordinary non-Go source: %q", got)
	}
	if strings.Contains(got, "SubmoduleSymbolMustNotLoad") {
		t.Fatalf("repo map descended into submodule: %q", got)
	}
}
