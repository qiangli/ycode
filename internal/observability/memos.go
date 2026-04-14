package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"

	memosembed "github.com/usememos/memos/embed"
)

// MemosComponent embeds the Memos note-taking server as an observability
// stack component. It runs on an ephemeral port and is reverse-proxied
// at /memos/ on the proxy landing page.
// Used as ycode's persistent long-term memory storage backend.
type MemosComponent struct {
	dataDir string
	port    int
	server  *memosembed.Server
	healthy atomic.Bool
}

// NewMemosComponent creates a component that runs the Memos server.
// dataDir is the directory for SQLite database and attachments.
func NewMemosComponent(dataDir string) *MemosComponent {
	return &MemosComponent{dataDir: dataDir}
}

func (m *MemosComponent) Name() string { return "memos" }

func (m *MemosComponent) Start(ctx context.Context) error {
	// Ensure data directory exists.
	if err := os.MkdirAll(m.dataDir, 0o755); err != nil {
		return fmt.Errorf("memos: create data dir: %w", err)
	}

	// Find an ephemeral port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("memos: listen: %w", err)
	}
	m.port = listener.Addr().(*net.TCPAddr).Port
	// Close the listener — the memos server will re-bind on this port.
	listener.Close()

	// Create and start the memos server.
	s, err := memosembed.NewServer(ctx, memosembed.Config{
		Addr:    "127.0.0.1",
		Port:    m.port,
		DataDir: m.dataDir,
		Driver:  "sqlite",
	})
	if err != nil {
		return fmt.Errorf("memos: init server: %w", err)
	}
	m.server = s

	if err := s.Start(ctx); err != nil {
		return fmt.Errorf("memos: start server: %w", err)
	}

	m.healthy.Store(true)
	slog.Info("memos: started", "port", m.port, "data", m.dataDir)
	return nil
}

func (m *MemosComponent) Stop(ctx context.Context) error {
	m.healthy.Store(false)
	if m.server != nil {
		m.server.Shutdown(ctx)
	}
	return nil
}

func (m *MemosComponent) Healthy() bool {
	return m.healthy.Load()
}

// HTTPHandler returns nil — Memos runs its own HTTP server.
// Accessed via reverse proxy from the stack manager.
func (m *MemosComponent) HTTPHandler() http.Handler {
	return nil
}

// Port returns the Memos HTTP port for reverse proxying.
func (m *MemosComponent) Port() int { return m.port }

// MemosAddr returns the full address (e.g. "127.0.0.1:12345") for direct access.
func (m *MemosComponent) MemosAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", m.port)
}
