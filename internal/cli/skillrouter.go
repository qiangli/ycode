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
// nadir scores the prompt against the registered slash commands, and
// if there's a clear winner the corresponding command runs directly.
// Otherwise the prompt falls through to the existing LLM agentic loop.
//
// Three backends, composable:
//
//   1. Lexical                 pure-Go TF-IDF over command Name+Description+Examples.
//                              Default — zero deps, microsecond Routes.
//   2. Ollama embedder         Real semantic embeddings (mxbai-embed-large
//                              etc.) via Ollama. Activated by
//                              YCODE_SKILL_ROUTER_EMBED_MODEL.
//   3. + LLM rerank            On top of either primary, route uncertain
//                              picks through a small Ollama LLM
//                              (qwen2.5:7b, llama3.2:1b, …) for a final
//                              verdict. Activated by
//                              YCODE_SKILL_ROUTER_LLM_MODEL.
//
// Combinations: (1) alone, (2) alone, (1)+(3), or (2)+(3). The best on
// our synthetic eval was Cascade(TF-IDF + qwen2.5:7b) at 97.1%, but
// Cascade(mxbai-embed-large + qwen2.5:7b) hasn't been measured and may
// edge it out — both primary signal AND rerank are stronger.
//
// Failure modes are silent: Hybrid construction errors fall back to
// the next-best configuration with a one-line warning. Routing always
// produces a result; the worst case is the original LLM agentic loop.

// skillRouterLLMTimeout caps the per-call LLM rerank deadline. Kept
// generous (8 s) since first-call cold-load of a 7B model on a fresh
// daemon can exceed nadir's default of 2 s. The Cascade falls back to
// the primary's verdict on timeout — never blocks routing.
const skillRouterLLMTimeout = 8 * time.Second

// primaryThresholds bundles the calibration knobs that differ between
// TF-IDF and Ollama-embedder primaries. Cosine baselines differ by
// ~3× between them — TF-IDF clusters in-catalog cosines around 0.6–0.9
// with OOD near 0.2, while mxbai-embed-large produces in-catalog
// ~0.7–0.85 and OOD ~0.5 — so the same float thresholds can't be
// shared.
type primaryThresholds struct {
	// Passed to nadir's NewSemantic — minimum top-1 cosine before
	// Decision.FellThrough fires.
	semanticMinScore float64
	// Passed to nadir's NewCascade — minimum top1-top2 gap before the
	// LLM rerank is consulted.
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
	// Tuned for mxbai-embed-large / nomic-embed-text style cosine
	// distributions (higher baseline, smaller spread). Re-tune if a
	// different embedder is used and routing feels off.
	ollamaThresholds = primaryThresholds{
		semanticMinScore:   0.55,
		cascadeMinMargin:   0.05,
		routeMinConfidence: 0.62,
		routeMinMargin:     0.05,
	}
)

// skillRouterUpgradeCooldown is how long we wait between probing
// Ollama for a degraded matcher to upgrade. 30 s strikes a balance:
// fast enough that the user sees the upgrade soon after a background
// pull finishes, slow enough that we don't hammer Ollama on every
// prompt during a cold start.
const skillRouterUpgradeCooldown = 30 * time.Second

// appSkillRouter holds the lazily-built matcher and the label used
// in log messages. Supports cold-start degradation: if the user
// configured an Ollama embedder but the model isn't pulled yet on
// first request, we build TF-IDF and serve from that, then re-probe
// every skillRouterUpgradeCooldown to upgrade to Ollama once the
// model lands. So the very first ycode prompt always routes, even
// while serve's background pull is still running.
type appSkillRouter struct {
	mu          sync.Mutex
	matcher     skillrouter.Matcher
	label       string // e.g. "lexical", "ollama:mxbai-embed-large:latest+qwen2.5:7b"
	thresh      primaryThresholds
	err         error
	wantsOllama bool      // user asked for Ollama embedder primary
	lastProbe   time.Time // last attempt to build/upgrade
}

func (a *App) skillRouterEnabled() bool {
	return os.Getenv("YCODE_SKILL_ROUTER") == "1"
}

// skillRouterEmbedModel reports the Ollama embedder to use as the
// primary. Empty → fall back to TF-IDF.
func skillRouterEmbedModel() string {
	return os.Getenv("YCODE_SKILL_ROUTER_EMBED_MODEL")
}

// skillRouterLLMModel reports the Ollama LLM to use as the Cascade
// rerank step. Empty → no LLM rerank (primary alone).
func skillRouterLLMModel() string {
	return os.Getenv("YCODE_SKILL_ROUTER_LLM_MODEL")
}

// ollamaBaseURL resolves the base URL for Ollama API calls. The same
// URL is used for both embed and LLM calls — Ollama serves both on
// the same daemon. Explicit overrides for either step short-circuit
// this fallback chain.
func skillRouterOllamaBaseURL() string {
	if v := os.Getenv("YCODE_SKILL_ROUTER_OLLAMA_BASE_URL"); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return strings.TrimSuffix(inference.DefaultOllamaURL(), "/")
}

// skillRouterEmbedBaseURL is the base URL nadir's OllamaEmbedder
// dials. It uses the native /api/embeddings endpoint, NOT the /v1
// OpenAI shim — so no /v1 suffix.
func skillRouterEmbedBaseURL() string {
	if v := os.Getenv("YCODE_SKILL_ROUTER_EMBED_BASE_URL"); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return skillRouterOllamaBaseURL()
}

// skillRouterLLMBaseURL is the base URL nadir's LLM client dials. It
// uses the OpenAI-compat shim Ollama exposes, so we append /v1.
func skillRouterLLMBaseURL() string {
	if v := os.Getenv("YCODE_SKILL_ROUTER_LLM_BASE_URL"); v != "" {
		return strings.TrimSuffix(v, "/") + "/v1"
	}
	return skillRouterOllamaBaseURL() + "/v1"
}

// skillRouterMatcher returns a memoised matcher built from the
// command registry. On first call it builds whatever primary is
// available (preferring the user's choice but falling back to TF-IDF
// if Ollama isn't ready yet). On subsequent calls — when the cached
// matcher is on TF-IDF but the user wanted Ollama — it re-probes
// every skillRouterUpgradeCooldown to swap in the preferred backend
// once the model finishes pulling.
func (a *App) skillRouterMatcher(ctx context.Context) (skillrouter.Matcher, string, primaryThresholds, error) {
	if a.skillRouterCache == nil {
		a.skillRouterCache = &appSkillRouter{
			wantsOllama: skillRouterEmbedModel() != "",
		}
	}
	c := a.skillRouterCache
	c.mu.Lock()
	defer c.mu.Unlock()

	// Decide if we should (re)build. Three cases:
	//   1. No matcher yet — always build.
	//   2. Matcher exists and we got what we wanted — return it.
	//   3. Matcher exists but we degraded — re-probe after cooldown.
	needsBuild := c.matcher == nil
	degraded := c.matcher != nil && c.wantsOllama && !strings.HasPrefix(c.label, "ollama:")
	cooldownOK := time.Since(c.lastProbe) >= skillRouterUpgradeCooldown
	if !needsBuild && !(degraded && cooldownOK) {
		return c.matcher, c.label, c.thresh, c.err
	}

	skills := buildSkillCatalogFromCommands(a.commands.List())
	if len(skills) == 0 {
		return nil, "", primaryThresholds{}, nil
	}

	primary, label, thresh, err := buildSkillPrimary(ctx, skills)
	c.lastProbe = time.Now()
	if err != nil {
		// First-build failure is a real error; degraded-retry failure
		// is silent (keep serving the TF-IDF matcher).
		if needsBuild {
			c.err = err
			return nil, "", primaryThresholds{}, err
		}
		return c.matcher, c.label, c.thresh, c.err
	}

	// Build the (possibly cascaded) matcher.
	var matcher skillrouter.Matcher = primary
	if llmModel := skillRouterLLMModel(); llmModel != "" {
		client := openai.New("ycode-skill-router-llm", skillRouterLLMBaseURL(), "")
		matcher = skillrouter.NewCascade(primary, client, llmModel,
			skillrouter.WithMinMargin(thresh.cascadeMinMargin),
			skillrouter.WithShortlistK(5),
			skillrouter.WithLLMTimeout(skillRouterLLMTimeout),
		)
		label = label + "+" + llmModel
	}

	// On a degraded retry, only swap if we actually got the preferred
	// backend this time (otherwise we'd be churning identical
	// configurations on every Route call).
	if degraded && !strings.HasPrefix(label, "ollama:") {
		return c.matcher, c.label, c.thresh, c.err
	}
	if degraded {
		// Announce the upgrade once — useful signal that the cold
		// start is over.
		fmt.Fprintf(a.stdout, "skill-router: upgraded to %s (Ollama models now ready)\n", label)
	}
	c.matcher = matcher
	c.label = label
	c.thresh = thresh
	c.err = nil
	return c.matcher, c.label, c.thresh, c.err
}

// buildSkillPrimary picks the primary embedder + Semantic matcher.
// Ollama embedder when configured; TF-IDF otherwise. Falls back to
// TF-IDF on Ollama probe failure (the embedder constructor probes
// once at build time to discover dimension — that doubles as a
// reachability check).
func buildSkillPrimary(ctx context.Context, skills []nadir.Skill) (*skillrouter.Semantic, string, primaryThresholds, error) {
	if embedModel := skillRouterEmbedModel(); embedModel != "" {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		emb, err := skillrouter.NewOllamaEmbedder(probeCtx, skillRouterEmbedBaseURL(), embedModel,
			skillrouter.WithOllamaTimeout(20*time.Second),
		)
		cancel()
		if err == nil {
			s, sErr := skillrouter.NewSemantic(ctx, emb, skills,
				skillrouter.WithMinScore(ollamaThresholds.semanticMinScore),
			)
			if sErr == nil {
				return s, "ollama:" + embedModel, ollamaThresholds, nil
			}
			err = sErr
		}
		// Hybrid embedder unavailable — log and fall through to TF-IDF
		// rather than refuse to route entirely.
		fmt.Fprintf(os.Stderr, "skill-router: ollama embedder %q unavailable (%v); falling back to TF-IDF\n",
			embedModel, err)
	}
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
