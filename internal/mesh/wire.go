package mesh

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/qiangli/ycode/internal/runtime/memory"
	"github.com/qiangli/ycode/internal/selfheal"
)

// WireDeps holds dependencies for wiring mesh agent callbacks.
type WireDeps struct {
	MemoryManager *memory.Manager
	Healer        *selfheal.Healer

	// SearchFunc performs a web search (e.g., SearXNG).
	// Returns formatted search results.
	SearchFunc func(ctx context.Context, query string) (string, error)
}

// WireCallbacks injects real implementations into mesh agents.
// Must be called after NewDefaultMesh() and before Mesh.Start().
func WireCallbacks(m *Mesh, deps *WireDeps) {
	if deps == nil {
		return
	}

	for _, agent := range m.agents {
		switch a := agent.(type) {
		case interface{ Unwrap() MeshAgent }:
			// TracedAgent wrapper — unwrap to get the real agent.
			wireAgent(a.Unwrap(), deps)
		default:
			wireAgent(a, deps)
		}
	}
}

func wireAgent(agent MeshAgent, deps *WireDeps) {
	switch a := agent.(type) {
	case *Fixer:
		wireFixer(a, deps)
	case *Researcher:
		wireResearcher(a, deps)
	case *Learner:
		wireLearner(a, deps)
	case *Trainer:
		wireTrainer(a, deps)
	}
}

func wireFixer(f *Fixer, deps *WireDeps) {
	if deps.Healer == nil {
		return
	}
	healer := deps.Healer
	f.FixFunc = func(ctx context.Context, report DiagnosticReport) (FixResult, error) {
		errInfo := selfheal.ErrorInfo{
			Type:      selfheal.ClassifyError(fmt.Errorf("%s", report.Summary)),
			Error:     fmt.Errorf("%s: %s", report.Category, report.Summary),
			Message:   report.Summary,
			Timestamp: report.Timestamp,
		}

		success, err := healer.AttemptHealing(ctx, errInfo)
		action := "skip"
		if success {
			action = "code_fix"
		}
		return FixResult{
			ReportID: report.ID,
			Success:  success,
			Action:   action,
			Detail:   fmt.Sprintf("healer result: success=%v", success),
		}, err
	}
	slog.Debug("mesh.wire: fixer wired to selfheal.Healer")
}

func wireResearcher(r *Researcher, deps *WireDeps) {
	if deps.SearchFunc != nil {
		r.SearchFunc = deps.SearchFunc
		slog.Debug("mesh.wire: researcher wired to search provider")
	}

	if deps.MemoryManager != nil {
		mgr := deps.MemoryManager
		r.SaveFunc = func(ctx context.Context, name, content string) error {
			mem := &memory.Memory{
				Name:        fmt.Sprintf("research_%s", name),
				Description: fmt.Sprintf("Research result: %s", name),
				Type:        memory.TypeReference,
				Content:     content,
			}
			return mgr.Save(mem)
		}
		slog.Debug("mesh.wire: researcher wired to memory manager")
	}
}

func wireLearner(l *Learner, deps *WireDeps) {
	if deps.MemoryManager == nil {
		return
	}
	mgr := deps.MemoryManager
	l.SaveMemoryFunc = func(ctx context.Context, name, memType, content string) error {
		t := memory.TypeProcedural
		switch memType {
		case "episodic":
			t = memory.TypeEpisodic
		case "reference":
			t = memory.TypeReference
		case "project":
			t = memory.TypeProject
		}
		mem := &memory.Memory{
			Name:        name,
			Description: fmt.Sprintf("Learned: %s", name),
			Type:        t,
			Content:     content,
		}
		return mgr.Save(mem)
	}
	slog.Debug("mesh.wire: learner wired to memory manager")
}

func wireTrainer(t *Trainer, deps *WireDeps) {
	// Training pipeline wired in Phase 6.
	// For now, set a no-op if not already set.
	if t.TrainFunc == nil {
		t.TrainFunc = func(ctx context.Context) (bool, float64, error) {
			slog.Info("mesh.trainer: training pipeline not yet configured")
			return false, 0, nil
		}
	}
}
