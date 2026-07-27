package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
)

// selectModel resolves the model this invocation will run.
//
// Precedence: --model flag > ANTHROPIC_MODEL > YCODE_MODEL > config > default,
// then cascade-ladder capture, fleet-selector resolution and alias expansion.
// When the selector names a cascade agent, the ladder is recorded on cfg (the
// selector is gone one line later, collapsed to its base model id).
//
// It lives here, apart from newApp, because readiness must be able to ask
// "which model would this invocation use?" WITHOUT constructing an App. When
// readiness answered that question separately it answered it differently, and
// a report about a model nobody selected is worse than no report.
func selectModel(cfg *config.Config) string {
	model := cfg.Model
	if envModel := os.Getenv("YCODE_MODEL"); envModel != "" {
		model = envModel
	}
	if envModel := os.Getenv("ANTHROPIC_MODEL"); envModel != "" {
		model = envModel
	}
	if modelFlag != "" {
		model = modelFlag
	}
	// A CASCADE agent (ycode-cascade-x4) is a ladder, not a model. Capture the
	// whole ladder here — this is the only point where the selector still
	// exists; one line down it has collapsed to the base model id, which is
	// precisely how a cascade ended up serving its cheapest rung for an entire
	// session. An explicit config ladder wins over the catalog's.
	if len(cfg.CascadeModels) == 0 {
		if ladder, ok := api.ResolveCascadeLadder(model); ok {
			cfg.CascadeModels = ladder
			slog.Info("cascade agent selected", "agent", model, "ladder", strings.Join(ladder, " → "))
		}
	}
	// A fleet selector (nickname / agent name / band like L3) resolves to a
	// concrete ycode model id; a literal model id passes through unchanged.
	if fm, note := api.ResolveFleetModel(model); fm != model {
		slog.Info("fleet model selection", "from", model, "resolved", note)
		model = fm
	}
	return api.ResolveModelWithAliases(model, cfg.Aliases)
}

// unattendedShouldReportAndExit decides what an unattended `ycode` with no
// prompt (no args, nothing piped) does: print a readiness report and exit, or
// open the steerable TUI.
//
//   - explicitFlags (--no-interactive / --yes) is a promise that no UI will
//     open. Always report and exit.
//   - otherwise the context is ambient (WEAVE_ID, CI=true). If a terminal is
//     attached, somebody allocated a pty and is going to type into it — that
//     is how `bashy coach` opens a steerable session — so open the TUI. With
//     no terminal there is nobody to type, and a report is all we can give.
func unattendedShouldReportAndExit(explicitFlags, stdinIsTerminal bool) bool {
	if explicitFlags {
		return true
	}
	return !stdinIsTerminal
}

// selectedModel loads config the way newApp does and returns the model this
// invocation would run. A config that fails to load is not fatal here: env and
// --model still decide, and a readiness report is worth printing either way.
func selectedModel() string {
	home, _ := os.UserHomeDir()
	userDir := filepath.Join(home, ".config", "ycode")
	cwd, _ := os.Getwd()
	projectDir := filepath.Join(cwd, ".agents", "ycode")

	cfg, err := config.NewLoader(userDir, projectDir, projectDir).Load()
	if err != nil || cfg == nil {
		cfg = &config.Config{}
	}
	return selectModel(cfg)
}
