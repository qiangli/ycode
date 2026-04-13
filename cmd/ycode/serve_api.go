package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/server"
	"github.com/qiangli/ycode/internal/service"
)

var (
	serveNoAPI  bool
	serveNoNATS bool
	apiPort     int
	apiHostname string
	apiNATSPort int
)

// apiStack holds the API server and NATS server state.
type apiStack struct {
	memBus  *bus.MemoryBus
	svc     *service.LocalService
	apiSrv  *server.Server
	natsSrv *server.NATSServer
	token   string
}

// startAPIStack initializes and starts the API server and optionally NATS.
func startAPIStack(noAPI, noNATS bool) (*apiStack, error) {
	app, err := newApp()
	if err != nil {
		return nil, err
	}

	memBus := bus.NewMemoryBus()
	svc := service.NewLocalService(app, memBus)

	stack := &apiStack{
		memBus: memBus,
		svc:    svc,
	}

	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	stack.token = token
	_ = writeTokenFile(token)

	if !noAPI {
		apiSrv := server.New(server.Config{
			Port:     apiPort,
			Hostname: apiHostname,
			Token:    token,
		}, svc)
		if err := apiSrv.Start(); err != nil {
			return nil, err
		}
		stack.apiSrv = apiSrv
	}

	if !noNATS {
		natsSrv := server.NewNATSServer(server.NATSConfig{
			Enabled:  true,
			Port:     apiNATSPort,
			Embedded: true,
		}, svc)
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
	if s.apiSrv != nil {
		s.apiSrv.Stop(context.Background())
	}
	if s.memBus != nil {
		s.memBus.Close()
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
	dir := filepath.Join(home, ".ycode")
	_ = os.MkdirAll(dir, 0o755)
	return os.WriteFile(filepath.Join(dir, "server.token"), []byte(token), 0o600)
}
