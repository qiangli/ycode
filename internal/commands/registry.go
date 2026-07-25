package commands

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/qiangli/ycode/internal/features"
)

// HandlerFunc is the function signature for a slash command handler.
type HandlerFunc func(ctx context.Context, args string) (string, error)

// Spec describes a slash command.
type Spec struct {
	Name        string
	Description string
	Usage       string
	Category    string
	Handler     HandlerFunc
	// AgentPrompt, when non-nil, chains into an agentic conversation turn
	// after the handler completes. The handler runs first (e.g. scaffold),
	// its output is displayed, then AgentPrompt is called to produce the
	// prompt sent to the LLM for the agentic turn.
	AgentPrompt func(args string) string

	// ShellSafe marks this command as runnable from `ycode shell` mode,
	// where there is no agent conversation / *App context. Handlers
	// that depend on RuntimeDeps fields like RunAgenticInit, RetryTurn,
	// or ModelSwitcher should leave this false — invoking them in shell
	// mode would dereference nil and crash the shell.
	ShellSafe bool

	// Examples lists representative natural-language invocations that
	// map to this command. Used by the optional pre-LLM skill router
	// (internal/cli/skillrouter.go) to fit per-command embedding
	// prototypes. 3–10 paraphrases is the sweet spot — short, varied,
	// no need to be exhaustive. Empty Examples is fine; the matcher
	// falls back to embedding Description alone, just with weaker
	// paraphrase coverage.
	Examples []string

	// Tier is the release-readiness label, reusing the vocabulary of
	// internal/features (stable | experimental | wip). Empty means stable.
	//
	// A non-stable command is HIDDEN: it does not appear in /help, inline
	// completion, the command palette, the shell manifest, or the skill
	// router's catalog. It remains dispatchable when typed explicitly, so
	// nothing is deleted and work in progress stays reachable.
	//
	// The label lives here rather than in features/registry.yaml because a
	// YAML entry keyed by name fails OPEN — rename the command and it
	// silently un-hides; add a new stub and it is visible by default. This
	// field fails closed, and the line you delete when the command becomes
	// real is in the same diff that made it real.
	Tier features.Tier
}

// Hidden reports whether this command is withheld from the user-facing
// listings. Dispatch ignores it: a hidden command still runs when typed.
func (s *Spec) Hidden() bool {
	return s.Tier != "" && s.Tier != features.TierStable
}

// Registry holds all registered slash commands.
type Registry struct {
	mu       sync.RWMutex
	commands map[string]*Spec
}

// NewRegistry creates a new command registry.
func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]*Spec),
	}
}

// Register adds a command to the registry.
func (r *Registry) Register(spec *Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[spec.Name] = spec
}

// Get returns a command by name.
func (r *Registry) Get(name string) (*Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.commands[name]
	return spec, ok
}

// Execute runs a command by name, hidden or not.
//
// Hidden commands stay runnable on purpose — hiding withdraws a recommendation,
// it does not remove the code. But running one prints what it is first: without
// that, an unlisted command looks like a working feature to whoever typed it,
// which is the very impression hiding exists to prevent.
func (r *Registry) Execute(ctx context.Context, name string, args string) (string, error) {
	spec, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown command: /%s", name)
	}
	out, err := spec.Handler(ctx, args)
	if err != nil || !spec.Hidden() {
		return out, err
	}
	return fmt.Sprintf("⚠ /%s is %s — output may be incomplete or a placeholder.\n\n%s",
		spec.Name, spec.Tier, out), nil
}

// ListAll returns every command sorted by name, hidden ones included.
//
// Reserved for inventory and diagnostics — tests that assert the full surface,
// and anything auditing what exists. USER-FACING SURFACES MUST USE List(): a
// listing built from ListAll advertises unfinished commands.
func (r *Registry) ListAll() []*Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]*Spec, 0, len(r.commands))
	for _, spec := range r.commands {
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// List returns the VISIBLE commands sorted by name.
//
// Filtering is the default, and deliberately so: this is the obvious name, so
// it is the one a new presentation surface will reach for. Making the safe
// behaviour the default means a future surface cannot advertise a stub by
// forgetting to filter.
func (r *Registry) List() []*Spec {
	all := r.ListAll()
	specs := make([]*Spec, 0, len(all))
	for _, spec := range all {
		if spec.Hidden() {
			continue
		}
		specs = append(specs, spec)
	}
	return specs
}

// ListAllByCategory groups every command by category, hidden ones included.
// Categories and the commands within them are both sorted, so callers can
// render deterministic output.
func (r *Registry) ListAllByCategory() map[string][]*Spec {
	return groupByCategory(r.ListAll())
}

// ListByCategory groups the VISIBLE commands by category. Same defaulting
// rationale as List.
func (r *Registry) ListByCategory() map[string][]*Spec {
	return groupByCategory(r.List())
}

// groupByCategory buckets an already-sorted spec list, preserving that order
// within each bucket. Callers still have to sort the category KEYS — a map has
// no order, and iterating one directly is what made /help's output differ run
// to run. Categories returns them sorted.
func groupByCategory(specs []*Spec) map[string][]*Spec {
	categories := make(map[string][]*Spec)
	for _, spec := range specs {
		cat := spec.Category
		if cat == "" {
			cat = "general"
		}
		categories[cat] = append(categories[cat], spec)
	}
	return categories
}

// Categories returns the category names of the given grouping, sorted.
func Categories(grouped map[string][]*Spec) []string {
	cats := make([]string, 0, len(grouped))
	for cat := range grouped {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	return cats
}
