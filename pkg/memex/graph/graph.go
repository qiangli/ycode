package graph

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	api "github.com/dgraph-io/dgo/v250/protos/api"
	"github.com/qiangli/bonsai/pkg/bonsai"
)

// Graph is the memex queryable graph store. Open returns one ready to
// accept Alter / Mutate / Query calls. Close shuts it down cleanly; calls
// after Close return an error from the underlying bonsai DB.
//
// Graph is safe for concurrent use; bonsai serializes mutations internally.
type Graph struct {
	db     *bonsai.DB
	opts   Options
	logger *slog.Logger

	closeOnce sync.Once
}

// Options configures Open.
type Options struct {
	// Dir is the on-disk location of the bonsai database. Created if it
	// doesn't exist. Required.
	Dir string

	// AutoSchema, when true, infers a permissive schema on first write
	// for any predicate not yet declared via Alter. Defaults to false:
	// callers must apply a schema explicitly for stricter validation.
	AutoSchema bool

	// CacheMB sets the bonsai/Badger cache size in megabytes. Zero leaves
	// bonsai at its default (typically modest).
	CacheMB int64

	// ValueLogGCInterval controls how often bonsai reclaims value-log
	// space. Zero leaves bonsai's default (10 minutes). Negative disables.
	ValueLogGCInterval time.Duration

	// ReadOnly opens the database without permitting mutations. Useful for
	// observers / replicas.
	ReadOnly bool

	// Logger receives structured events. Defaults to slog.Default().
	Logger *slog.Logger
}

// Open creates or opens a bonsai-backed graph at opts.Dir. The directory
// is created if missing. The default schema declared by Schema() is
// applied; callers may apply additional Alter steps afterward.
func Open(opts Options) (*Graph, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("graph: Options.Dir is required")
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("graph: create dir: %w", err)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	db, err := bonsai.Open(bonsai.Options{
		Dir:                opts.Dir,
		CacheMB:            opts.CacheMB,
		ReadOnly:           opts.ReadOnly,
		AutoSchema:         opts.AutoSchema,
		ValueLogGCInterval: opts.ValueLogGCInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("graph: open bonsai at %s: %w", opts.Dir, err)
	}

	g := &Graph{
		db:     db,
		opts:   opts,
		logger: logger,
	}

	if !opts.ReadOnly {
		if err := g.Alter(context.Background(), Schema()); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("graph: apply default schema: %w", err)
		}
	}

	return g, nil
}

// Close shuts down the underlying bonsai DB. Safe to call multiple times;
// subsequent calls return nil.
func (g *Graph) Close() error {
	var err error
	g.closeOnce.Do(func() {
		err = g.db.Close()
	})
	return err
}

// Alter applies a DQL schema change. Idempotent for already-declared
// predicates; rebuilds indexes when index directives change.
func (g *Graph) Alter(ctx context.Context, schema string) error {
	if err := g.db.Alter(ctx, schema); err != nil {
		return fmt.Errorf("graph: alter schema: %w", err)
	}
	return nil
}

// Query runs a DQL query and returns the raw JSON response body. Vars may
// be nil. Empty vars and nil vars are equivalent.
func (g *Graph) Query(ctx context.Context, dql string, vars map[string]string) ([]byte, error) {
	var resp *api.Response
	var err error
	if len(vars) == 0 {
		resp, err = g.db.Query(ctx, dql)
	} else {
		resp, err = g.db.QueryWithVars(ctx, dql, vars)
	}
	if err != nil {
		return nil, fmt.Errorf("graph: query: %w", err)
	}
	return resp.GetJson(), nil
}

// Mutate inserts edges expressed as N-Quads RDF. The returned map
// translates blank-node labels (_:foo) to assigned UIDs.
func (g *Graph) Mutate(ctx context.Context, nquads []byte) (map[string]string, error) {
	resp, err := g.db.Mutate(ctx, &api.Mutation{SetNquads: nquads, CommitNow: true})
	if err != nil {
		return nil, fmt.Errorf("graph: mutate: %w", err)
	}
	return resp.GetUids(), nil
}

// Upsert runs queryDQL and applies nquads in one atomic transaction. The
// queryDQL block typically defines variables that nquads references for
// idempotent insert-or-update semantics.
func (g *Graph) Upsert(ctx context.Context, queryDQL string, nquads []byte) error {
	if _, err := g.db.Upsert(ctx, queryDQL, &api.Mutation{SetNquads: nquads, CommitNow: true}); err != nil {
		return fmt.Errorf("graph: upsert: %w", err)
	}
	return nil
}

// Export streams the entire database to w in the given format ("rdf" or
// "json"). Useful for backup and migration.
func (g *Graph) Export(ctx context.Context, w io.Writer, format string) error {
	if err := g.db.ExportTo(ctx, format, w); err != nil {
		return fmt.Errorf("graph: export: %w", err)
	}
	return nil
}

// HTTPHandler returns the bonsai REST + Explorer UI handler. Mountable
// on any HTTP mux. May return nil if bonsai's HTTP layer is unavailable
// in this build.
func (g *Graph) HTTPHandler() http.Handler {
	// bonsai exposes its HTTP routes through cmd/bonsai/server, not
	// directly off *DB. The umbrella memex package is responsible for
	// constructing that handler when it wants the wiki UI; here we
	// expose a simple JSON query endpoint as a fallback so embedders
	// have something usable without pulling cmd/bonsai/server.
	return jsonQueryHandler(g)
}

// DB returns the underlying bonsai DB. Escape hatch for callers that need
// methods not surfaced here (BackupTo, namespaces, point-in-time queries).
// Stable to use, but not part of the memex semver promise.
func (g *Graph) DB() *bonsai.DB { return g.db }

// MirrorTarget is a tag method that lets internal subsystems use *Graph as
// a mirror sink without importing pkg/memex/graph directly. Defined as a
// no-op method so the structural type satisfies a small interface declared
// at the use site (e.g. internal/runtime/codegraph.MirrorSink).
func (g *Graph) MirrorTarget() {}
