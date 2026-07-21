package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/cascade"
	"github.com/qiangli/ycode/internal/runtime/conversation"
)

// EnvCascadeModels overrides the ladder without touching config or the fleet
// catalog: YCODE_CASCADE_MODELS="glm-5.2,gpt-5.6-terra,gpt-5.6-sol".
const EnvCascadeModels = "YCODE_CASCADE_MODELS"

// cascadeEscalator returns this session's escalator, building it on first use.
// nil when no ladder is configured — an ordinary single-model run.
func (a *App) cascadeEscalator() *cascade.Escalator {
	a.escalatorOnce.Do(func() {
		ladder := cascadeLadderFromEnv()
		if len(ladder) == 0 && a.config != nil {
			ladder = a.config.CascadeModels
		}
		a.escalator = cascade.New(cascade.Config{
			Ladder: ladder,
			// A tier we have no key for is not a tier. Probing at switch time
			// (rather than at startup) keeps the check honest: a key can be
			// rotated in or out mid-session.
			Probe: func(model string) error {
				_, _, err := a.providerForModel(model)
				return err
			},
			OnSwitch: func(sw cascade.Switch) {
				a.emitEvent("cascade.escalate", map[string]any{
					"from":   sw.From,
					"to":     sw.To,
					"rung":   sw.Rung,
					"reason": sw.Reason,
				})
			},
		})
		if a.escalator != nil {
			slog.Info("cascade escalation armed",
				"base", a.escalator.Base(), "ladder", strings.Join(a.escalator.Ladder(), " → "))
		}
	})
	return a.escalator
}

// providerForModel resolves a model id to a live provider and its display kind.
// It is the single place a cascade rung is turned into something that can speak,
// and the seam tests replace to exercise escalation without provider keys.
func (a *App) providerForModel(model string) (api.Provider, string, error) {
	if a.providerFactory != nil {
		return a.providerFactory(model)
	}
	cfg, err := api.DetectProvider(model)
	if err != nil {
		return nil, "", err
	}
	return api.NewProvider(cfg), cfg.DisplayKind(), nil
}

func cascadeLadderFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv(EnvCascadeModels))
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// observeCascade feeds one turn's outcome to the escalator and applies the
// result. It is the single place a stuck run changes models.
//
// Reporting is the point. A successful escalation is announced (the operator
// paid for a premium tier and deserves to see it); an escalation that could not
// happen — top rung already, or no reachable tier — is announced too, because
// the alternative is what shipped before: a run that quietly grinds on the
// model that is already stuck and exits looking fine.
func (a *App) observeCascade(rt *conversation.Runtime, sig cascade.Signal) {
	e := a.cascadeEscalator()
	if e == nil {
		return
	}
	sw, err := e.Observe(sig)
	if err != nil {
		a.reportCascadeFailure(e, sig, err)
		return
	}
	if sw == nil {
		return
	}
	a.applyEscalation(rt, *sw)
}

// escalatedModel is the model the cascade is currently serving, or "" when
// there is no cascade. Nil-safe.
func (a *App) escalatedModel() string {
	if a == nil || a.escalator == nil {
		return ""
	}
	if !a.escalator.Escalated() {
		return ""
	}
	return a.escalator.Model()
}

// applyEscalation re-points the runtime (and, when the rung crosses providers,
// the provider) at the escalated model.
//
// config.Model is deliberately left on the BASE: the action recorder derives
// escalated = served_model != base_model, so leaving the base in place is what
// makes the premium turns self-identify in actions.jsonl and on the agent.turn
// span.
// It reports whether the switch actually took effect.
func (a *App) applyEscalation(rt *conversation.Runtime, sw cascade.Switch) bool {
	provider, kind, err := a.providerForModel(sw.To)
	if err != nil {
		// Probe said this tier was reachable a moment ago; if it isn't now,
		// say so rather than pretending the switch happened.
		fmt.Fprintf(a.chromeWriter(), "\n✘ Cascade: cannot reach %s (%v) — staying on %s.\n\n", sw.To, err, sw.From)
		slog.Error("cascade: escalation target unusable", "model", sw.To, "error", err)
		return false
	}
	a.provider = provider
	a.providerKind = kind

	if rt != nil {
		rt.Escalate(sw.To, sw.Reason, provider)
	}

	fmt.Fprintf(a.chromeWriter(), "\n⇧ Cascade escalation: %s → %s (rung %d, reason: %s)\n\n",
		sw.From, sw.To, sw.Rung, sw.Reason)
	slog.Warn("cascade: served model escalated",
		"from", sw.From, "to", sw.To, "rung", sw.Rung, "reason", sw.Reason, "provider", a.providerKind)
	return true
}

// reportCascadeFailure surfaces a warranted-but-impossible escalation.
func (a *App) reportCascadeFailure(e *cascade.Escalator, sig cascade.Signal, err error) {
	fmt.Fprintf(a.chromeWriter(), "\n✘ Cascade: %s detected on %s but no escalation is available: %v\n\n",
		sig, e.Model(), err)
	slog.Error("cascade: escalation unavailable — the run continues on a model that is already stuck",
		"signal", sig.String(), "model", e.Model(), "error", err)
	a.emitEvent("cascade.unavailable", map[string]any{
		"signal": sig.String(),
		"model":  e.Model(),
		"error":  err.Error(),
	})
}

// escalateAndContinue is the hard-loop path: the response detector says the
// agent is stuck for good. If a stronger rung exists, take it, clear the
// detector's history (the new model's output must be judged on its own), and
// report true so the caller keeps going. false means there was nothing left to
// escalate to and the loop really does have to be broken.
func (a *App) escalateAndContinue(rt *conversation.Runtime, detector *conversation.LoopDetector) bool {
	e := a.cascadeEscalator()
	if e == nil {
		return false
	}
	sw, err := e.Observe(cascade.SignalLoop)
	if err != nil {
		a.reportCascadeFailure(e, cascade.SignalLoop, err)
		return false
	}
	if sw == nil {
		return false
	}
	if !a.applyEscalation(rt, *sw) {
		return false // the switch did not take; do not pretend it did
	}
	detector.Reset()
	return true
}

// turnSignal classifies a completed turn for the escalator: a turn whose tool
// calls ALL failed made no progress, and so did a turn that produced neither
// text nor tools. Anything else counts as progress.
func turnSignal(result *conversation.TurnResult, toolResults []api.ContentBlock) cascade.Signal {
	if result == nil {
		return cascade.SignalStall
	}
	var calls, failures int
	for _, blk := range toolResults {
		if blk.Type != api.ContentTypeToolResult {
			continue
		}
		calls++
		if blk.IsError {
			failures++
		}
	}
	if calls > 0 && failures == calls {
		return cascade.SignalStall
	}
	if calls == 0 && strings.TrimSpace(result.TextContent) == "" {
		return cascade.SignalStall
	}
	return cascade.SignalProgress
}
