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
// nadir scores the prompt against the registered slash commands, and
// if there's a clear winner the corresponding command runs directly.
// Otherwise the prompt falls through to the existing LLM agentic loop.
//
// The matcher is pure-Go TF-IDF over command Name+Description+Examples.
// Routing always produces a result; the worst case is the original LLM
// agentic loop.

// primaryThresholds bundles the calibration knobs used by the TF-IDF
// semantic matcher.
type primaryThresholds struct {
	// Passed to nadir's NewSemantic — minimum top-1 cosine before
	// Decision.FellThrough fires.
	semanticMinScore float64
	// Reserved for matcher variants that need a top1-top2 gap before a
	// secondary pass.
	cascadeMinMargin float64
	// ycode-level confidence floor: even if nadir says "go", we only
	// route when Decision.Confidence clears this. Conservative on
	// purpose — routing the wrong command is worse than deferring to
	// the LLM, which can still call the same command as a tool.
	routeMinConfidence float64
	// ycode-level margin floor on the embedding-side gap.
	routeMinMargin float64
}

var (
	tfidfThresholds = primaryThresholds{
		semanticMinScore:   0.25,
		cascadeMinMargin:   0.10,
		routeMinConfidence: 0.40,
		routeMinMargin:     0.15,
	}
)

// appSkillRouter holds the lazily-built matcher and the label used in
// log messages.
type appSkillRouter struct {
	mu      sync.Mutex
	matcher skillrouter.Matcher
	label   string
	thresh  primaryThresholds
	err     error
}

func (a *App) skillRouterEnabled() bool {
	return os.Getenv("YCODE_SKILL_ROUTER") == "1"
}

// skillRouterMatcher returns a memoised matcher built from the
// command registry.
func (a *App) skillRouterMatcher(ctx context.Context) (skillrouter.Matcher, string, primaryThresholds, error) {
	if a.skillRouterCache == nil {
		a.skillRouterCache = &appSkillRouter{}
	}
	c := a.skillRouterCache
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.matcher != nil {
		return c.matcher, c.label, c.thresh, c.err
	}

	skills := buildSkillCatalogFromCommands(a.commands.List())
	if len(skills) == 0 {
		return nil, "", primaryThresholds{}, nil
	}

	primary, label, thresh, err := buildSkillPrimary(ctx, skills)
	if err != nil {
		c.err = err
		return nil, "", primaryThresholds{}, err
	}
	c.matcher = primary
	c.label = label
	c.thresh = thresh
	c.err = nil
	return c.matcher, c.label, c.thresh, c.err
}

// buildSkillPrimary picks the primary embedder + Semantic matcher.
// It is TF-IDF-only in the lean agent build.
func buildSkillPrimary(ctx context.Context, skills []nadir.Skill) (*skillrouter.Semantic, string, primaryThresholds, error) {
	s, err := skillrouter.NewSemantic(ctx, skillrouter.NewTFIDFFromSkills(skills), skills,
		skillrouter.WithMinScore(tfidfThresholds.semanticMinScore),
	)
	return s, "lexical", tfidfThresholds, err
}

// buildSkillCatalogFromCommands maps the command Specs into nadir's
// Skill shape. Examples are passed through and become per-prototype
// vectors in the embedding space; Description provides a fallback
// when no Examples are populated yet. Commands without a Description
// AND without Examples are skipped.
func buildSkillCatalogFromCommands(specs []*commands.Spec) []nadir.Skill {
	out := make([]nadir.Skill, 0, len(specs))
	for _, s := range specs {
		if s.Name == "" {
			continue
		}
		if s.Description == "" && len(s.Examples) == 0 {
			continue
		}
		out = append(out, nadir.Skill{
			Name:        s.Name,
			Description: s.Description,
			Examples:    s.Examples,
		})
	}
	return out
}

// trySkillRouter attempts to route a freeform prompt to one of the
// registered slash commands. Returns (commandName, true) iff the
// matcher returns a high-confidence pick.
func (a *App) trySkillRouter(ctx context.Context, prompt string) (string, bool) {
	if !a.skillRouterEnabled() {
		return "", false
	}
	matcher, label, thresh, err := a.skillRouterMatcher(ctx)
	if err != nil {
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
	if d.Confidence < thresh.routeMinConfidence || d.Margin < thresh.routeMinMargin {
		fmt.Fprintf(a.stdout, "skill-router[%s]: uncertain pick %q (conf=%.2f margin=%.2f) — deferring to LLM\n",
			label, d.Skill, d.Confidence, d.Margin)
		return "", false
	}
	name := strings.TrimPrefix(d.Skill, "/")
	fmt.Fprintf(a.stdout, "skill-router[%s]: routed to /%s (conf=%.2f margin=%.2f)\n",
		label, name, d.Confidence, d.Margin)
	return name, true
}
