package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/cli"
	"github.com/qiangli/ycode/internal/inference"
	internalserver "github.com/qiangli/ycode/internal/server"
	"github.com/qiangli/ycode/internal/service"
)

var (
	serveNoAPI  bool
	serveNoNATS bool
	apiNATSPort int
)

// apiStack holds the API/NATS server state for the unified serve command.
type apiStack struct {
	app      *cli.App              // primary app (first session or server-started)
	pool     *service.SessionPool  // session pool for multi-project support
	multiSvc *service.MultiService // multi-session service
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

	// Build a session pool with a factory that creates new App instances per workDir.
	pool := service.NewSessionPool(func(workDir string) (service.AppBackend, error) {
		return newApp(workDir)
	})

	// Seed the pool with the primary app's session.
	pool.SeedSession(app)

	// Create the multi-session service.
	multiSvc := service.NewMultiService(pool, memBus)
	multiSvc.SetOllamaLister(ollamaLister)

	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	_ = writeTokenFile(token)

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
