package api

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/qiangli/coreutils/pkg/fleet"
)

// bandRE matches a capability-band selector: L3, l3, b3, band:3, band=3, band 3.
var bandRE = regexp.MustCompile(`(?i)^(?:l|b|band)[\s:=]*([1-9])$`)

// ResolveFleetModel maps a fleet SELECTOR to the concrete model id ycode accepts,
// so the model can be named the way the fleet is named instead of only by a raw
// provider id:
//
//   - a capability band  (L3 / b3 / band:3)   → the strongest ycode-runnable model at/above it
//   - an agent name/nick bound to ycode        → that agent's model
//   - a fleet model name or family alias       → its ycode id (kimi → kimi-k3)
//
// Only ycode-RUNNABLE models (those with a ycode: agent binding) are ever
// considered, so a band pick can never hand ycode a subscription-only model it
// has no key for.
//
// A value that matches no selector is returned UNCHANGED, so an ordinary model id
// (deepseek-chat, claude-sonnet-4-6) passes straight through — the resolution is
// purely additive and cannot break an existing invocation. The second return is a
// short human note for logging, empty when nothing was resolved.
func ResolveFleetModel(sel string) (string, string) {
	raw := strings.TrimSpace(sel)
	if raw == "" {
		return sel, ""
	}
	cat := fleet.New()

	// 1. A band selector → the strongest ycode-runnable model at or above it.
	if mm := bandRE.FindStringSubmatch(raw); mm != nil {
		band, _ := strconv.Atoi(mm[1])
		if id, name := bestYcodeModel(cat, band); id != "" {
			return id, fmt.Sprintf("%s → %s (%s)", fleet.BandLabel(band), name, id)
		}
		return sel, "" // no ycode model at that band; leave it literal
	}

	// 2. An agent name or nickname bound to ycode → its model's ycode id.
	if a, ok := cat.Agent(raw); ok && a.Tool == "ycode" {
		if _, _, m, err := cat.Binding(a.Name); err == nil {
			id := m.TargetFor("ycode")
			return id, fmt.Sprintf("%s → %s (%s)", raw, m.Name, id)
		}
	}

	// 3. A fleet model name or family alias → its ycode id, if ycode can run it.
	if m, ok := cat.Model(raw); ok && ycodeRuns(cat, m.Name) {
		if id := m.TargetFor("ycode"); id != raw {
			return id, fmt.Sprintf("%s → %s", raw, id)
		}
	}

	return sel, "" // not a fleet selector: a literal model id
}

// ResolveCascadeLadder returns the ordered ycode model ids behind a CASCADE
// agent selector — the base first, then each escalation rung — so ycode can run
// the ladder itself instead of only ever serving the base.
//
// A cascade agent (ycode-cascade-x4: glm-5.2 → terra → sol) declares
// `base` + `escalation` in the fleet catalog. Resolving `--model
// ycode-cascade-x4` through ResolveFleetModel yields only the NOMINAL model,
// which is the base — correct for starting the run, and exactly why a cascade
// used to spend an entire session on its cheapest rung. This exposes the rest
// of the ladder so the runtime can climb it when the base is demonstrably
// stuck.
//
// ok=false when sel is not a cascade agent (a plain model id, a band, a
// non-cascade agent) — callers then run the single model as before. Rungs that
// do not resolve to a ycode-runnable model are skipped rather than failing the
// whole ladder: a partially-usable ladder still beats none.
func ResolveCascadeLadder(sel string) ([]string, bool) {
	raw := strings.TrimSpace(sel)
	if raw == "" {
		return nil, false
	}
	cat := fleet.New()
	a, ok := cat.Agent(raw)
	if !ok || !a.IsCascade() {
		return nil, false
	}

	var models []string
	seen := map[string]bool{}
	for _, rung := range append([]string{a.Base}, a.Escalation...) {
		id := ycodeModelForAgent(cat, rung)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, id)
	}
	if len(models) < 2 {
		// A one-rung "ladder" is just a model; nothing to escalate to.
		return nil, false
	}
	return models, true
}

// ycodeModelForAgent resolves an agent name to the model id ycode speaks, or ""
// when the agent is unknown or not ycode-runnable.
func ycodeModelForAgent(cat *fleet.Catalog, name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	a, _, m, err := cat.Binding(name)
	if err != nil || a.Tool != "ycode" {
		return ""
	}
	return m.TargetFor("ycode")
}

// bestYcodeModel returns the id + fleet name of the strongest ycode-runnable model
// pegged at or above minBand (highest band, then name for determinism).
func bestYcodeModel(cat *fleet.Catalog, minBand int) (id, name string) {
	agents, _ := cat.Agents()
	var best fleet.Model
	found := false
	for _, a := range agents {
		if a.Tool != "ycode" {
			continue
		}
		_, _, m, err := cat.Binding(a.Name)
		if err != nil || m.Band < minBand {
			continue
		}
		if !found || m.Band > best.Band || (m.Band == best.Band && m.Name < best.Name) {
			best, found = m, true
		}
	}
	if !found {
		return "", ""
	}
	return best.TargetFor("ycode"), best.Name
}

// ycodeRuns reports whether ycode has a binding for the named model — the gate
// that keeps model-name selection inside the set ycode actually has keys for.
func ycodeRuns(cat *fleet.Catalog, modelName string) bool {
	agents, _ := cat.Agents()
	for _, a := range agents {
		if a.Tool == "ycode" && a.Model == modelName {
			return true
		}
	}
	return false
}
