package mesh

import (
	"log/slog"
	"time"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/tools"
)

// NewDefaultMesh creates a mesh with all agents configured for the given mode.
func NewDefaultMesh(cfg *MeshConfig, b bus.Bus, qm *tools.QualityMonitor) *Mesh {
	m := New(cfg, b)
	safety := NewSafetyGuard(cfg.MaxFixAttempts, 2)

	// Always register the diagnoser.
	diagnoser := NewDiagnoser(b, qm, cfg.DiagInterval)
	m.Register(NewTracedAgent(diagnoser))

	// Always register the learner.
	learner := NewLearner(b, 10*time.Minute)
	m.Register(NewTracedAgent(learner))

	if cfg.Mode == "server" {
		// Server mode: add fixer, researcher, trainer.
		fixer := NewFixer(b, safety)
		m.Register(NewTracedAgent(fixer))

		researcher := NewResearcher(b, cfg.ResearchLimit)
		m.Register(NewTracedAgent(researcher))

		if cfg.TrainingEnabled {
			trainer := NewTrainer(b, 0) // uses default 24h interval
			m.Register(NewTracedAgent(trainer))
		}
	}

	slog.Info("mesh.coordinator.configured",
		"mode", cfg.Mode,
		"agent_count", len(m.agents),
	)

	return m
}
