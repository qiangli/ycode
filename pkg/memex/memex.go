package memex

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/qiangli/ycode/pkg/memex/memory"
	"github.com/qiangli/ycode/pkg/memex/memos"
	"github.com/qiangli/ycode/pkg/memex/store"
	"github.com/qiangli/ycode/pkg/memex/store/kv"
	"github.com/qiangli/ycode/pkg/memex/store/sqlite"
)

// Memex bundles the memex subpackages behind a single handle. All accessors
// are safe for concurrent use; close once via Close when shutting down.
type Memex struct {
	cfg     config
	storage *store.Manager
	memory  *memory.Manager
	memos   memos.Store
	vfs     VFS
	handler http.Handler
}

type config struct {
	globalDir string
	embedder  store.EmbeddingFunc
	logger    *slog.Logger
}

// Option configures Open.
type Option func(*config)

// WithGlobalDir sets the directory for global-scope memory entries
// (defaults to <dir>/memory if unset, same as project-scope memory).
func WithGlobalDir(path string) Option {
	return func(c *config) { c.globalDir = path }
}

// WithEmbedder supplies an embedding function for the vector store. The
// vector store is only initialized when an embedder is provided.
func WithEmbedder(fn store.EmbeddingFunc) Option {
	return func(c *config) { c.embedder = fn }
}

// WithLogger sets a custom slog.Logger. Defaults to slog.Default().
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// Open creates a Memex rooted at dir. KV opens synchronously; SQL is
// initialized synchronously here (so memos can be backed immediately) and
// the vector store is wired only if WithEmbedder is set.
//
// dir is created if it does not exist. Memory files live at dir/memory and
// SQL/KV/search/vector backends live alongside in dir.
func Open(dir string, opts ...Option) (*Memex, error) {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx := context.Background()

	storeCfg := store.Config{
		DataDir: dir,
		KVFactory: func(ctx context.Context) (store.KVStore, error) {
			return kv.Open(dir)
		},
		SQLFactory: func(ctx context.Context) (store.SQLStore, error) {
			return sqlite.Open(dir)
		},
	}
	sm, err := store.NewManager(ctx, storeCfg)
	if err != nil {
		return nil, fmt.Errorf("memex open: store: %w", err)
	}

	if err := sm.InitSQL(ctx); err != nil {
		_ = sm.Close()
		return nil, fmt.Errorf("memex open: sql: %w", err)
	}

	memoryDir := filepath.Join(dir, "memory")
	var mem *memory.Manager
	if cfg.globalDir != "" {
		mem, err = memory.NewManagerWithGlobal(cfg.globalDir, memoryDir)
	} else {
		mem, err = memory.NewManager(memoryDir)
	}
	if err != nil {
		_ = sm.Close()
		return nil, fmt.Errorf("memex open: memory: %w", err)
	}

	var memoStore memos.Store
	if sql := sm.SQL(ctx); sql != nil {
		memoStore = memos.NewSQLStore(sql)
	}

	m := &Memex{
		cfg:     cfg,
		storage: sm,
		memory:  mem,
		memos:   memoStore,
	}
	m.vfs = NewVFS(mem, memoStore)
	if memoStore != nil {
		m.handler = memos.NewWebHandler(memoStore)
	}
	return m, nil
}

// Store returns the persistence Manager (KV/SQL/search/vector).
func (m *Memex) Store() *store.Manager { return m.storage }

// Memory returns the file-based memory Manager.
func (m *Memex) Memory() *memory.Manager { return m.memory }

// Memos returns the wiki-notes Store. May be nil if SQL initialization failed.
func (m *Memex) Memos() memos.Store { return m.memos }

// VFS returns the unified virtual filesystem over memory + memos.
func (m *Memex) VFS() VFS { return m.vfs }

// HTTPHandler returns the memos REST + web UI handler. May be nil if Memos
// is nil. Mountable on any HTTP mux.
func (m *Memex) HTTPHandler() http.Handler { return m.handler }

// Close shuts down all underlying backends.
func (m *Memex) Close() error {
	if m.storage != nil {
		return m.storage.Close()
	}
	return nil
}
