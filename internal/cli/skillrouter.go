package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/nadir"
	"github.com/qiangli/nadir/provider/openai"
	"github.com/qiangli/nadir/skillrouter"

	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/inference"
)

// Skill-router integration — prototype.
//
// When YCODE_SKILL_ROUTER=1, freeform user prompts (no leading slash,
// no high-confidence builtin intent) get a pre-LLM routing pass:
// nadir's TF-IDF matcher scores them against the registered slash
// commands, and if there's a clear winner (top-1 score ≥ MinScore,
// margin ≥ MinMargin), the corresponding command runs directly.
// Otherwise the prompt falls through to the existing LLM agentic loop.
//
// Two backends are available:
//
//   - Lexical (default)  pure-Go TF-IDF. Zero deps, microsecond
//     Route calls, ~94% top-1 on a synthetic eval corpus.
//   - Hybrid (Ollama)    TF-IDF primary + Ollama LLM rerank when the
//     primary is uncertain. Activated by YCODE_SKILL_ROUTER_LLM_MODEL.
//     ~97% top-1 with qwen2.5:7b; base URL respects OLLAMA_HOST and
//     defaults to ycode's inference.DefaultOllamaURL().
//
// Default is off — opt-in only. When enabled, the App logs every
// intercepted route so the user can see what the matcher chose and
// why. Failure modes are silent (matcher errors are logged but the
// prompt always falls through to the existing path).
//
// To extend: enrich Spec with an `Examples []string` field so the
// TF-IDF prototypes have richer paraphrase coverage. Today we fit
// from Name+Description only — sufficient for clear intent matches
// like "review my PR" → /review, but synonym handling is weaker.

// skillRouterRouteThreshold is the minimum top-1 score required to
// auto-route. Calibrated for TF-IDF over short command Descriptions —
// a confident match clears 0.4; OOD prompts cluster near 0.
const skillRouterRouteThreshold = 0.40

// skillRouterMinMargin is the minimum top1-top2 gap required to
// auto-route. Without it, ambiguous prompts ("review and audit")
// would route to whichever command's vocabulary happened to overlap
// slightly more — better to fall through to the LLM in that case.
const skillRouterMinMargin = 0.15

// skillRouterLLMTimeout caps the per-call LLM rerank deadline. Kept
// generous (8 s) since first-call cold-load of a 7B model on a fresh
// daemon can exceed the nadir default of 2 s. The Cascade falls back
// to the primary's verdict on timeout — never blocks routing.
const skillRouterLLMTimeout = 8 * time.Second

// skillRouter is the lazily-initialised matcher for the App. nil
// until first use; one instance per process. The catalog is rebuilt
// each time RegisterCommand fires, which means a freshly-registered
// command isn't routable until the next time skillRouterMatch runs.
// For the prototype that's fine — commands are registered once at
// startup.
type appSkillRouter struct {
	once    sync.Once
	matcher skillrouter.Matcher
	label   string // "lexical" or "hybrid:<model>"
	err     error
}

func (a *App) skillRouterEnabled() bool {
	return os.Getenv("YCODE_SKILL_ROUTER") == "1"
}

// skillRouterLLMModel returns the configured rerank model, if any.
// Presence of this env var switches the router from Lexical to
// Hybrid mode.
func skillRouterLLMModel() string {
	return os.Getenv("YCODE_SKILL_ROUTER_LLM_MODEL")
}

// skillRouterLLMBaseURL resolves the Ollama base URL used for the
// LLM rerank step. Explicit override wins; otherwise we use ycode's
// existing DefaultOllamaURL() which respects OLLAMA_HOST and falls
// back to http://127.0.0.1:11434. The "/v1" OpenAI-compat suffix is
// appended here so the matcher can rely on the OpenAI-compatible
// chat endpoint nadir uses.
func skillRouterLLMBaseURL() string {
	if v := os.Getenv("YCODE_SKILL_ROUTER_LLM_BASE_URL"); v != "" {
		return strings.TrimSuffix(v, "/") + "/v1"
	}
	return strings.TrimSuffix(inference.DefaultOllamaURL(), "/") + "/v1"
}

// skillRouterMatcher returns a memoised matcher built from the
// command registry. Built once; if the catalog is empty or
// construction fails, returns (nil, nil) and the caller treats it as
// "skip routing for this prompt".
func (a *App) skillRouterMatcher(ctx context.Context) (skillrouter.Matcher, string, error) {
	if a.skillRouterCache == nil {
		a.skillRouterCache = &appSkillRouter{}
	}
	a.skillRouterCache.once.Do(func() {
		skills := buildSkillCatalogFromCommands(a.commands.List())
		if len(skills) == 0 {
			return
		}
		// Hybrid path: requires an LLM model configured. Falls back
		// silently to Lexical if construction fails (e.g., Ollama not
		// reachable at startup).
		if model := skillRouterLLMModel(); model != "" {
			client := openai.New("ycode-skill-router", skillRouterLLMBaseURL(), "")
			m, err := nadir.NewHybridSkillMatcher(ctx, client, model, skills)
			if err == nil {
				a.skillRouterCache.matcher = m
				a.skillRouterCache.label = "hybrid:" + model
				return
			}
			// Fall through to Lexical on Hybrid construction failure
			// rather than disabling routing entirely. The error gets
			// surfaced as a one-line warning when first used.
			a.skillRouterCache.err = fmt.Errorf("hybrid init failed (%w); using lexical fallback", err)
		}
		m, err := nadir.NewLexicalSkillMatcher(skills)
		if err != nil {
			a.skillRouterCache.err = err
			return
		}
		a.skillRouterCache.matcher = m
		if a.skillRouterCache.label == "" {
			a.skillRouterCache.label = "lexical"
		}
	})
	return a.skillRouterCache.matcher, a.skillRouterCache.label, a.skillRouterCache.err
}

// buildSkillCatalogFromCommands maps the command Specs into nadir's
// Skill shape. Name and Description are taken directly; Examples is
// empty (no Spec field for it today). The matcher's TF-IDF still
// works from Description alone — accuracy is weaker on paraphrases
// but on-vocabulary queries route reliably.
//
// Commands without a Description are skipped — they'd embed as a
// single-vector prototype on Name only, which is mostly noise for a
// lexical matcher.
func buildSkillCatalogFromCommands(specs []*commands.Spec) []nadir.Skill {
	out := make([]nadir.Skill, 0, len(specs))
	for _, s := range specs {
		if s.Name == "" || s.Description == "" {
			continue
		}
		out = append(out, nadir.Skill{
			Name:        s.Name,
			Description: s.Description,
		})
	}
	return out
}

// trySkillRouter attempts to route a freeform prompt to one of the
// registered slash commands. Returns (commandName, true) iff the
// matcher returns a high-confidence pick. Logs the decision either
// way for transparency.
//
// The threshold/margin tuning is intentionally conservative: routing
// the wrong command is much worse than letting the LLM handle it,
// since the LLM can still call the same command as a tool. We only
// intercept on clear wins.
func (a *App) trySkillRouter(ctx context.Context, prompt string) (string, bool) {
	if !a.skillRouterEnabled() {
		return "", false
	}
	matcher, label, err := a.skillRouterMatcher(ctx)
	if err != nil {
		// One-shot warning so the user knows we degraded silently.
		fmt.Fprintf(a.stdout, "skill-router: %v\n", err)
	}
	if matcher == nil {
		return "", false
	}
	d, err := matcher.Route(ctx, prompt)
	if err != nil {
		fmt.Fprintf(a.stdout, "skill-router: matcher error: %v (falling through)\n", err)
		return "", false
	}
	if d == nil || d.FellThrough {
		return "", false
	}
	if d.Confidence < skillRouterRouteThreshold || d.Margin < skillRouterMinMargin {
		fmt.Fprintf(a.stdout, "skill-router[%s]: uncertain pick %q (conf=%.2f margin=%.2f) — deferring to LLM\n",
			label, d.Skill, d.Confidence, d.Margin)
		return "", false
	}
	// Strip any leading slash on the skill name — registry keys are
	// bare names ("init", "review"), not "/init".
	name := strings.TrimPrefix(d.Skill, "/")
	fmt.Fprintf(a.stdout, "skill-router[%s]: routed to /%s (conf=%.2f margin=%.2f)\n",
		label, name, d.Confidence, d.Margin)
	return name, true
}
