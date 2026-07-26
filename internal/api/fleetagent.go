package api

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/qiangli/coreutils/pkg/fleet"
)

// FleetAgent is the ycode-facing view of a fleet agent: enough to list it,
// pick it, and report what you switched into. It is deliberately flat — the
// full fleet types stay behind this package so the rest of ycode does not
// grow a dependency on the catalog's shape.
type FleetAgent struct {
	Name    string // canonical agent name, e.g. codex-gpt-5.5
	Nick    string // human handle, e.g. Arlo
	Tool    string // the CLI that runs it, e.g. codex
	Model   string // fleet model name
	Binding string // "tool:model"
	Band    int    // capability band, 1..4
	Cascade bool   // an escalation ladder rather than a single model
}

// Label renders an agent for a one-line report.
func (a FleetAgent) Label() string {
	if a.Nick != "" {
		return fmt.Sprintf("%s (%s, %s)", a.Nick, a.Binding, fleet.BandLabel(a.Band))
	}
	return fmt.Sprintf("%s (%s)", a.Name, fleet.BandLabel(a.Band))
}

// ListFleetAgents returns every agent in the catalog, strongest band first
// and then by name so the ordering is stable enough to render.
func ListFleetAgents() []FleetAgent {
	cat := fleet.New()
	agents, _ := cat.Agents()

	out := make([]FleetAgent, 0, len(agents))
	for _, a := range agents {
		out = append(out, newFleetAgent(cat, a))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Band != out[j].Band {
			return out[i].Band > out[j].Band
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ResolveFleetAgent maps a selector to a concrete agent:
//
//   - an agent name, nickname or alias   → that agent
//   - a capability band (L3 / b3 / band:3) → the strongest agent at or above it
//
// It differs from ResolveFleetModel in two ways that matter.
//
// First, an unresolvable selector is an ERROR, not a passthrough. A raw model
// id has to survive ResolveFleetModel untouched because ycode must still accept
// one; but there is no such fallback for an agent — "switch me to codx" has no
// sensible literal meaning, and silently doing nothing is worse than saying so.
//
// Second, a band selector PREFERS A NON-YCODE TOOL at equal band. The point of
// switching agents is to leave ycode; resolving `L4` back into ycode would be a
// no-op that looks like it worked.
func ResolveFleetAgent(sel string) (FleetAgent, error) {
	raw := strings.TrimSpace(sel)
	if raw == "" {
		return FleetAgent{}, fmt.Errorf("no agent given")
	}
	cat := fleet.New()

	if mm := bandRE.FindStringSubmatch(raw); mm != nil {
		band, _ := strconv.Atoi(mm[1])
		if a, ok := bestAgentAtBand(cat, band); ok {
			return a, nil
		}
		return FleetAgent{}, fmt.Errorf("no operable agent at %s or above", fleet.BandLabel(band))
	}

	if a, ok := cat.Agent(raw); ok {
		return newFleetAgent(cat, a), nil
	}

	return FleetAgent{}, unknownAgentError(raw)
}

// YcodeAgentForModel finds the ycode agent bound to a model id — the reverse
// lookup, used to describe the CURRENT session (which knows its model id, not
// which fleet agent it is).
func YcodeAgentForModel(modelID string) (FleetAgent, bool) {
	id := strings.TrimSpace(modelID)
	if id == "" {
		return FleetAgent{}, false
	}
	cat := fleet.New()
	agents, _ := cat.Agents()
	for _, a := range agents {
		if a.Tool != "ycode" {
			continue
		}
		_, _, m, err := cat.Binding(a.Name)
		if err != nil {
			continue
		}
		if m.TargetFor("ycode") == id || m.Name == id {
			return newFleetAgent(cat, a), true
		}
	}
	return FleetAgent{}, false
}

// newFleetAgent projects a catalog agent, resolving its band through the
// binding.
func newFleetAgent(cat *fleet.Catalog, a fleet.Agent) FleetAgent {
	fa := FleetAgent{
		Name:    a.Name,
		Nick:    a.NickName(),
		Tool:    a.Tool,
		Model:   a.Model,
		Binding: a.MatrixKey(),
		Cascade: a.IsCascade(),
	}
	fa.Band = agentBand(cat, a)
	return fa
}

// agentBand returns the band an agent actually SERVES.
//
// For an ordinary agent that is its model's band. For a CASCADE it is the
// agent's own Band field: a cascade starts on a cheap base and climbs, so the
// base model's band understates it — reading Model.Band for a cascade reports
// the floor as though it were the ceiling.
func agentBand(cat *fleet.Catalog, a fleet.Agent) int {
	if a.IsCascade() {
		return a.Band
	}
	if _, _, m, err := cat.Binding(a.Name); err == nil {
		return m.Band
	}
	return a.Band
}

// bestAgentAtBand picks the strongest agent at or above minBand, preferring a
// tool other than ycode (see ResolveFleetAgent), then the higher band, then the
// name for determinism.
func bestAgentAtBand(cat *fleet.Catalog, minBand int) (FleetAgent, bool) {
	var best FleetAgent
	found := false
	for _, cand := range listAgents(cat) {
		if cand.Band < minBand {
			continue
		}
		if !found || betterAgent(cand, best) {
			best, found = cand, true
		}
	}
	return best, found
}

func betterAgent(cand, best FleetAgent) bool {
	candLeaves := cand.Tool != "ycode"
	bestLeaves := best.Tool != "ycode"
	if candLeaves != bestLeaves {
		return candLeaves
	}
	if cand.Band != best.Band {
		return cand.Band > best.Band
	}
	return cand.Name < best.Name
}

func listAgents(cat *fleet.Catalog) []FleetAgent {
	agents, _ := cat.Agents()
	out := make([]FleetAgent, 0, len(agents))
	for _, a := range agents {
		out = append(out, newFleetAgent(cat, a))
	}
	return out
}

// unknownAgentError names the closest candidates, because the useful reply to
// a typo is the name the user meant.
func unknownAgentError(sel string) error {
	var near []string
	lower := strings.ToLower(sel)
	for _, a := range ListFleetAgents() {
		if strings.Contains(strings.ToLower(a.Name), lower) ||
			strings.Contains(strings.ToLower(a.Nick), lower) ||
			strings.Contains(strings.ToLower(a.Tool), lower) {
			near = append(near, a.Name)
		}
		if len(near) == 5 {
			break
		}
	}
	if len(near) > 0 {
		return fmt.Errorf("unknown agent %q; did you mean: %s", sel, strings.Join(near, ", "))
	}
	// No substring hit — a typo like "codx" matches nothing. Naming the
	// strongest few is still more use than sending the reader to another
	// command to find out what exists.
	var top []string
	for _, a := range ListFleetAgents() {
		top = append(top, a.Name)
		if len(top) == 5 {
			break
		}
	}
	return fmt.Errorf("unknown agent %q; try one of: %s (full list: `bashy agents list`)",
		sel, strings.Join(top, ", "))
}

// ResolveFleetTool maps a tool selector (claude, codex, opencode…) to the tool
// name the launcher accepts, erroring when the fleet has no such tool. Tools
// carry their own preconfigured defaults, so switching by TOOL is the "use it
// the way it is set up" path, distinct from naming a full tool:model agent.
func ResolveFleetTool(sel string) (string, error) {
	raw := strings.TrimSpace(sel)
	if raw == "" {
		return "", fmt.Errorf("no tool given")
	}
	cat := fleet.New()
	if t, ok := cat.Tool(raw); ok {
		return t.Name, nil
	}

	var names []string
	tools, _ := cat.Tools(false)
	for _, t := range tools {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return "", fmt.Errorf("unknown tool %q; known tools: %s", raw, strings.Join(names, ", "))
}
