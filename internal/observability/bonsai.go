package observability

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	bonsaiui "github.com/qiangli/bonsai/pkg/ui"

	"github.com/qiangli/ycode/pkg/memex/graph"
)

// BonsaiComponent exposes the memex queryable graph store as an
// observability stack component. Mounted on the reverse proxy at /graph/.
//
// The HTTP surface combines:
//   - /graph/        bonsai's embedded Explorer UI (DQL editor + schema sidebar)
//   - /graph/query   POST endpoint accepting {"dql": "...", "vars": {...}}
//
// The component owns its own bonsai instance. Bonsai/Badger holds a
// single-writer lock per directory, so a one-shot CLI invocation cannot
// open the same directory while serve is running; in that case Start
// returns an error and the component reports unhealthy (matching how the
// bbolt KV store handles the same constraint).
type BonsaiComponent struct {
	dataDir string
	g       *graph.Graph
	handler http.Handler
	healthy atomic.Bool
}

// NewBonsaiComponent creates a component that opens a bonsai graph at
// dataDir on Start. Stop closes it.
func NewBonsaiComponent(dataDir string) *BonsaiComponent {
	return &BonsaiComponent{dataDir: dataDir}
}

func (b *BonsaiComponent) Name() string { return "bonsai" }

func (b *BonsaiComponent) Start(ctx context.Context) error {
	g, err := graph.Open(graph.Options{Dir: b.dataDir})
	if err != nil {
		return err
	}
	b.g = g

	mux := http.NewServeMux()

	// /query — JSON-over-HTTP DQL endpoint shipped by pkg/memex/graph.
	mux.Handle("/query", b.g.HTTPHandler())

	// Catch-all → bonsai's embedded UI (single index.html).
	uiHandler := bonsaiui.Handler()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || r.URL.Path == "/" {
			r.URL.Path = "/"
		} else if strings.HasPrefix(r.URL.Path, "/graph") {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/graph")
		}
		uiHandler.ServeHTTP(w, r)
	})

	b.handler = mux
	b.healthy.Store(true)
	slog.Info("bonsai: started (memex graph)", "data", b.dataDir, "explorer", "/graph/", "query", "/graph/query")
	return nil
}

func (b *BonsaiComponent) Stop(ctx context.Context) error {
	b.healthy.Store(false)
	if b.g != nil {
		return b.g.Close()
	}
	return nil
}

func (b *BonsaiComponent) Healthy() bool { return b.healthy.Load() }

// HTTPHandler returns the combined Explorer + query handler.
func (b *BonsaiComponent) HTTPHandler() http.Handler { return b.handler }

// Graph returns the underlying graph store.
func (b *BonsaiComponent) Graph() *graph.Graph { return b.g }
