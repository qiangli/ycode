package observability

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/qiangli/ycode/internal/memos"
	"github.com/qiangli/ycode/internal/storage/sqlite"
)

// MemosComponent provides persistent memo storage as an observability
// stack component. Backed by ycode's SQLite database with FTS5 indexing.
// Mounted on the reverse proxy at /memos/.
type MemosComponent struct {
	dataDir string
	db      *sqlite.Store
	store   memos.Store
	handler http.Handler
	healthy atomic.Bool
}

// NewMemosComponent creates a component that provides memo storage.
// dataDir is the directory containing the SQLite database.
func NewMemosComponent(dataDir string) *MemosComponent {
	return &MemosComponent{dataDir: dataDir}
}

func (m *MemosComponent) Name() string { return "memos" }

func (m *MemosComponent) Start(ctx context.Context) error {
	db, err := sqlite.Open(m.dataDir)
	if err != nil {
		return err
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return err
	}
	m.db = db
	m.store = memos.NewSQLStore(db)
	m.handler = memos.NewWebHandler(m.store)
	m.healthy.Store(true)
	slog.Info("memos: started (internal store)", "data", m.dataDir)
	return nil
}

func (m *MemosComponent) Stop(ctx context.Context) error {
	m.healthy.Store(false)
	if m.db != nil {
		m.db.Close()
	}
	return nil
}

func (m *MemosComponent) Healthy() bool {
	return m.healthy.Load()
}

// HTTPHandler returns the web UI + API handler for in-process mounting.
func (m *MemosComponent) HTTPHandler() http.Handler {
	return m.handler
}

// Store returns the memo store for direct use by tools.
func (m *MemosComponent) Store() memos.Store {
	return m.store
}
