package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/qiangli/nadir"
	"github.com/qiangli/nadir/skillrouter"

	"github.com/qiangli/ycode/internal/commands"
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

// skillRouter is the lazily-initialised matcher for the App. nil
// until first use; one instance per process. The catalog is rebuilt
// each time RegisterCommand fires, which means a freshly-registered
// command isn't routable until the next time skillRouterMatch runs.
// For the prototype that's fine — commands are registered once at
// startup.
type appSkillRouter struct {
	once    sync.Once
	matcher skillrouter.Matcher
	err     error
}

func (a *App) skillRouterEnabled() bool {
	return os.Getenv("YCODE_SKILL_ROUTER") == "1"
}

// skillRouterMatcher returns a memoised matcher built from the
// command registry. Built once; if the catalog is empty or
// construction fails, returns (nil, nil) and the caller treats it as
// "skip routing for this prompt".
func (a *App) skillRouterMatcher() (skillrouter.Matcher, error) {
	if a.skillRouterCache == nil {
		a.skillRouterCache = &appSkillRouter{}
	}
	a.skillRouterCache.once.Do(func() {
		skills := buildSkillCatalogFromCommands(a.commands.List())
		if len(skills) == 0 {
			return
		}
		m, err := nadir.NewLexicalSkillMatcher(skills)
		if err != nil {
			a.skillRouterCache.err = err
			return
		}
		a.skillRouterCache.matcher = m
	})
	return a.skillRouterCache.matcher, a.skillRouterCache.err
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
	matcher, err := a.skillRouterMatcher()
	if err != nil {
		fmt.Fprintf(a.stdout, "skill-router: init failed: %v (falling through)\n", err)
		return "", false
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
		fmt.Fprintf(a.stdout, "skill-router: uncertain pick %q (conf=%.2f margin=%.2f) — deferring to LLM\n",
			d.Skill, d.Confidence, d.Margin)
		return "", false
	}
	// Strip any leading slash on the skill name — registry keys are
	// bare names ("init", "review"), not "/init".
	name := strings.TrimPrefix(d.Skill, "/")
	fmt.Fprintf(a.stdout, "skill-router: routed to /%s (conf=%.2f margin=%.2f)\n",
		name, d.Confidence, d.Margin)
	return name, true
}

