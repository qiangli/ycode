package mesh

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/tools"
)

func TestWireCallbacksFixer(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	qm := tools.NewQualityMonitor(0.7)
	m := NewDefaultMesh(DefaultMeshConfig(), b, qm)

	// Wire with nil healer — should not panic.
	WireCallbacks(m, &WireDeps{})

	// Verify fixer still has nil FixFunc (no healer to wire).
	for _, agent := range m.agents {
		if ta, ok := agent.(*TracedAgent); ok {
			if f, ok := ta.Unwrap().(*Fixer); ok {
				if f.FixFunc != nil {
					t.Fatal("expected nil FixFunc without healer")
				}
			}
		}
	}
}

func TestWireCallbacksResearcher(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	qm := tools.NewQualityMonitor(0.7)

	cfg := DefaultMeshConfig()
	cfg.Mode = "server" // server mode to get all agents
	m := NewDefaultMesh(cfg, b, qm)

	WireCallbacks(m, &WireDeps{
		SearchFunc: func(ctx context.Context, query string) (string, error) {
			return "search results for: " + query, nil
		},
	})

	// Verify researcher has SearchFunc wired.
	for _, agent := range m.agents {
		if ta, ok := agent.(*TracedAgent); ok {
			if r, ok := ta.Unwrap().(*Researcher); ok {
				if r.SearchFunc == nil {
					t.Fatal("expected SearchFunc to be wired")
				}
				result, err := r.SearchFunc(context.Background(), "test query")
				if err != nil {
					t.Fatalf("SearchFunc: %v", err)
				}
				if result != "search results for: test query" {
					t.Fatalf("unexpected result: %s", result)
				}
			}
		}
	}
}

func TestWireMeshStatusBeforeStart(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	qm := tools.NewQualityMonitor(0.7)
	m := NewDefaultMesh(DefaultMeshConfig(), b, qm)

	status := m.Status()
	if len(status) == 0 {
		t.Fatal("expected agents in status")
	}

	// All agents should be unhealthy before Start.
	for name, healthy := range status {
		if healthy {
			t.Fatalf("agent %s should be unhealthy before start", name)
		}
	}
}
