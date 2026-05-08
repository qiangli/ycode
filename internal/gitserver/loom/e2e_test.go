//go:build integration

package loom_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	giteacmd "code.gitea.io/gitea/cmd"

	"github.com/qiangli/ycode/internal/gitserver"
	gitserverloom "github.com/qiangli/ycode/internal/gitserver/loom"
	"github.com/qiangli/ycode/internal/gitserver/merger"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/pkg/loom"
)

// TestMain dispatches Gitea hook subcommands to Gitea's CLI; required
// for the embedded Gitea instance to function (its pre-receive hook
// invokes this binary).
func TestMain(m *testing.M) {
	for _, arg := range os.Args[1:] {
		if arg == "hook" {
			app := giteacmd.NewMainApp(giteacmd.AppVersion{Version: "test"})
			if err := giteacmd.RunMainApp(app, os.Args...); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

// TestE2E_Loom_FullFlow drives one sub-agent through the entire loom
// pipeline against a real embedded Gitea: lease → write file → push
// → merge → wait for merger to auto-merge → release.
func TestE2E_Loom_FullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (real embedded Gitea)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	giteaDir := t.TempDir()
	srv, err := gitserver.NewServer(&gitserver.ServerConfig{
		DataDir:  giteaDir,
		HTTPOnly: true,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = srv.Stop(stopCtx)
	})

	if _, err := srv.EnsureAdmin(ctx, "admin", "admin@ycode.local", gitserver.RandomPassword()); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	token, err := srv.IssueToken(ctx, "admin", "ycode-loom-e2e")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	client := gitserver.NewClient(srv.BaseURL(), token)

	hostCwd := newHostProject(t, "README.md", "host\n")
	registry, err := projects.NewRegistry(giteaDir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Wire up an auto-merge merger via OnProjectActive. CICommand="" means
	// merge unconditionally, which is fine for an isolated test repo.
	mergerCtx, mergerCancel := context.WithCancel(ctx)
	defer mergerCancel()
	logHandler := slog.New(slog.NewTextHandler(testLogWriter{t: t}, nil))
	onActive := func(_ context.Context, slug, cloneURL string) error {
		project := registry.Get(hostCwd)
		if project == nil || project.Slug != slug {
			t.Logf("OnProjectActive: skipping merger for slug %q (project not registered)", slug)
			return nil
		}
		syncLog, err := projects.NewSyncLog(giteaDir, project)
		if err != nil {
			return err
		}
		m, err := merger.New(merger.Config{
			Client:    client,
			Project:   project,
			SyncLog:   syncLog,
			CloneURL:  cloneURL,
			Token:     token,
			CICommand: "",
			WorkDir:   filepath.Join(giteaDir, "loom-merger-work-"+slug),
			Logger:    logHandler,
		})
		if err != nil {
			return err
		}
		go func() {
			_ = m.Run(mergerCtx, time.Second)
		}()
		return nil
	}

	backend, err := gitserverloom.NewGiteaBackend(gitserverloom.GiteaBackendOptions{
		Client:          client,
		Registry:        registry,
		Token:           token,
		OnProjectActive: onActive,
		Logger:          logHandler,
	})
	if err != nil {
		t.Fatalf("NewGiteaBackend: %v", err)
	}
	svc, err := loom.NewService(loom.Options{
		Backend:        backend,
		SandboxRoot:    filepath.Join(giteaDir, "loom-sandboxes"),
		ReaperInterval: time.Hour,
		Logger:         logHandler,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()

	// Lease.
	lease, err := svc.Lease(ctx, loom.LeaseRequest{
		CWD:           hostCwd,
		SubAgentLabel: "single",
	})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if !strings.HasPrefix(lease.Branch, "agent/agent-loom:single-") {
		t.Errorf("unexpected branch: %s", lease.Branch)
	}
	if _, err := os.Stat(lease.Path); err != nil {
		t.Fatalf("sandbox dir: %v", err)
	}

	// Sub-agent's "work" — a sentinel file inside the sandbox.
	if err := os.WriteFile(filepath.Join(lease.Path, "loom-sentinel.txt"),
		[]byte("written by loom e2e\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// Push.
	push, err := svc.Push(ctx, loom.PushRequest{LoomID: lease.ID, Message: "loom: e2e sentinel"})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !push.Pushed || push.CommitSHA == "" {
		t.Errorf("Push: %+v", push)
	}

	// Merge.
	merge, err := svc.Merge(ctx, loom.MergeRequest{LoomID: lease.ID, Title: "e2e sentinel"})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if merge.PRNumber <= 0 {
		t.Errorf("Merge: %+v", merge)
	}

	// Wait for the merger to flip status to merged.
	deadline := time.Now().Add(60 * time.Second)
	merged := false
	for time.Now().Before(deadline) {
		statuses, err := svc.Status(ctx, loom.StatusRequest{LoomID: lease.ID})
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if len(statuses) == 1 && statuses[0].State == loom.StateMerged {
			merged = true
			break
		}
		time.Sleep(time.Second)
	}
	if !merged {
		// Dump the lease's status for diagnosis.
		statuses, _ := svc.Status(ctx, loom.StatusRequest{LoomID: lease.ID})
		js, _ := json.MarshalIndent(statuses, "", "  ")
		t.Fatalf("PR did not merge within deadline. Status: %s", js)
	}

	// Verify the merge commit's author trailer carries the loom prefix.
	verifyDir := t.TempDir()
	if _, err := gitClone(ctx, verifyDir, lease.CloneURL, token); err != nil {
		t.Fatalf("verify clone: %v", err)
	}
	logOut, err := runCmd(verifyDir, "git", "log", "--all", "--format=%an", "main")
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, logOut)
	}
	if !strings.Contains(logOut, "agent-loom:single-") {
		t.Errorf("expected loom-prefixed author in main log, got:\n%s", logOut)
	}

	// Release.
	if err := svc.Release(ctx, loom.ReleaseRequest{LoomID: lease.ID}); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(lease.Path); !os.IsNotExist(err) {
		t.Errorf("sandbox not removed after Release: err=%v", err)
	}
}

// helpers ----------------------------------------------------------------

func newHostProject(t *testing.T, filename, content string) string {
	t.Helper()
	dir := t.TempDir()
	mustExec(t, dir, "git", "init", "-b", "main")
	mustExec(t, dir, "git", "config", "user.name", "host")
	mustExec(t, dir, "git", "config", "user.email", "host@example.com")
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	mustExec(t, dir, "git", "add", filename)
	mustExec(t, dir, "git", "commit", "-m", "initial")
	return dir
}

func mustExec(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
}

func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func gitClone(ctx context.Context, dir, cloneURL, token string) (string, error) {
	authURL := injectToken(cloneURL, token)
	cmd := exec.CommandContext(ctx, "git", "clone", "--branch", "main", "--single-branch", authURL, dir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func injectToken(rawURL, token string) string {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	scheme := "http://"
	rest := strings.TrimPrefix(rawURL, scheme)
	if rest == rawURL {
		scheme = "https://"
		rest = strings.TrimPrefix(rawURL, scheme)
	}
	return scheme + "token:" + token + "@" + rest
}

type testLogWriter struct {
	t *testing.T
}

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
