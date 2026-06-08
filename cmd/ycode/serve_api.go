package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/cli"
	"github.com/qiangli/ycode/internal/inference"
	internalserver "github.com/qiangli/ycode/internal/server"
	"github.com/qiangli/ycode/internal/service"
)

var (
	serveNoAPI           bool
	serveNoNATS          bool
	serveNoAuth          bool
	serveNoPersona       bool
	serveToolsAllowlist  []string
	serveToolsBlocklist  []string
	serveMCPPermission   string
	serveWorkspacePolicy string
	apiNATSPort          int
)

// apiStack holds the API/NATS server state for the unified serve command.
type apiStack struct {
	app      *cli.App              // primary app (first session or server-started)
	pool     *service.SessionPool  // session pool for multi-project support
	multiSvc *service.MultiService // multi-session service
	resolver *service.WorkspaceResolver
	memBus   *bus.MemoryBus
	svc      service.Service // the active service (LocalService or MultiService)
	srv      *internalserver.Server
	handler  http.Handler
	natsSrv  *internalserver.NATSServer
	token    string
}

// buildAPIStack initializes the app, service layer, and optionally NATS.
// It returns an apiStack with the HTTP handler ready to be served
// (by WebUIComponent or standalone).
func buildAPIStack(noNATS bool) (*apiStack, error) {
	// Create the primary app for the server's working directory.
	app, err := newApp()
	if err != nil {
		return nil, err
	}

	memBus := bus.NewMemoryBus()
	ollamaLister := inference.NewOllamaLister()
	cloudboxLister := api.NewCloudboxLister(
		os.Getenv("DHNT_BASE_URL"),
		os.Getenv("DHNT_API_KEY"),
		nil,
	)

	// The remote permission prompter routes elevated-tool checks over the bus
	// to whichever client is attached to the session (web UI, VS Code
	// extension, ...). multiSvc is created below; we capture it via a pointer
	// so the closure can be installed on apps before multiSvc exists. By the
	// time a tool actually invokes the prompter, multiSvc is set.
	var multiSvc *service.MultiService
	installRemotePrompter := func(b service.AppBackend) {
		b.InstallRemotePermissionPrompter(func(ctx context.Context, sessionID, toolName, mode string, input json.RawMessage) (bool, error) {
			if multiSvc == nil {
				return false, fmt.Errorf("permission requester not yet ready")
			}
			return multiSvc.RequestPermission(ctx, sessionID, toolName, mode, input)
		})
	}

	// Build a session pool with a factory that creates new App instances per
	// workDir, with the remote prompter installed before any tool runs.
	pool := service.NewSessionPool(func(workDir string) (service.AppBackend, error) {
		b, err := newApp(workDir)
		if err != nil {
			return nil, err
		}
		installRemotePrompter(b)
		return b, nil
	})

	// Seed the pool with the primary app's session.
	installRemotePrompter(app)
	pool.SeedSession(app)

	// Create the multi-session service.
	multiSvc = service.NewMultiService(pool, memBus)
	multiSvc.SetOllamaLister(ollamaLister)
	multiSvc.SetCloudboxLister(cloudboxLister)

	// Workspace resolver — decides what working directory a new web
	// session attaches to when the client doesn't pin one explicitly.
	// Default (per-session) allocates a fresh dir under
	// ~/.agents/ycode/workspaces/<owner>/<wsID>/; cwd preserves the
	// pre-policy behavior. The TUI / primary app keep using the
	// server's startup cwd regardless — the policy only affects
	// /api/sessions on the web path.
	startupCwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	workspacesRoot := filepath.Join(home, ".agents", "ycode", "workspaces")
	resolver := service.NewWorkspaceResolver(
		service.WorkspacePolicy(serveWorkspacePolicy),
		workspacesRoot,
		startupCwd,
	)
	multiSvc.SetWorkspaceResolver(resolver)

	// Token is used by the server's authMiddleware. When --no-auth is set
	// (or the operator otherwise wants permissive mode), we skip token
	// generation and the middleware becomes a pass-through. The token file
	// is still written when present so foreign clients can discover it.
	var token string
	if !serveNoAuth {
		var err error
		token, err = generateToken()
		if err != nil {
			return nil, err
		}
		_ = writeTokenFile(token)
	} else {
		// Best-effort: remove a stale token from a prior auth-enabled run
		// so clients don't try to use it.
		if home, err := os.UserHomeDir(); err == nil {
			_ = os.Remove(filepath.Join(home, ".agents", "ycode", "server.token"))
		}
	}

	// Build the HTTP/WebSocket handler (but don't start listening yet).
	srv := internalserver.New(internalserver.Config{Token: token}, multiSvc)

	stack := &apiStack{
		app:      app,
		pool:     pool,
		multiSvc: multiSvc,
		memBus:   memBus,
		svc:      multiSvc,
		srv:      srv,
		handler:  srv.Mux(),
		token:    token,
	}

	if !noNATS {
		natsSrv := internalserver.NewNATSServer(internalserver.NATSConfig{
			Enabled:  true,
			Port:     apiNATSPort,
			Embedded: true,
		}, multiSvc)
		if err := natsSrv.Start(context.Background()); err != nil {
			return nil, err
		}
		stack.natsSrv = natsSrv
	}

	return stack, nil
}

func (s *apiStack) stop() {
	if s.natsSrv != nil {
		s.natsSrv.Stop()
	}
	if s.memBus != nil {
		s.memBus.Close()
	}
	if s.multiSvc != nil {
		s.multiSvc.Close()
	} else if s.app != nil {
		s.app.Close()
	}
}

// generateToken creates a random 32-byte hex token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// writeTokenFile persists the auth token for clients to read.
func writeTokenFile(token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".agents", "ycode")
	_ = os.MkdirAll(dir, 0o755)
	return os.WriteFile(filepath.Join(dir, "server.token"), []byte(token), 0o600)
}
