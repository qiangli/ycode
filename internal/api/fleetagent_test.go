package api

import (
	"strings"
	"testing"
)

// These run against the REAL embedded fleet catalog, which is the point: the
// resolver's job is to agree with the catalog the rest of the fleet uses, and a
// mock would only prove it agrees with itself.

func TestListFleetAgentsIsOrderedAndPopulated(t *testing.T) {
	agents := ListFleetAgents()
	if len(agents) == 0 {
		t.Fatal("no agents in the embedded catalog")
	}
	for i := 1; i < len(agents); i++ {
		prev, cur := agents[i-1], agents[i]
		if cur.Band > prev.Band {
			t.Fatalf("agents are not band-descending: %s (L%d) precedes %s (L%d)",
				prev.Name, prev.Band, cur.Name, cur.Band)
		}
		if cur.Band == prev.Band && cur.Name < prev.Name {
			t.Fatalf("equal-band agents are not name-sorted: %q before %q", prev.Name, cur.Name)
		}
	}
	for _, a := range agents {
		if a.Name == "" || a.Tool == "" {
			t.Errorf("incomplete agent: %+v", a)
		}
	}
}

func TestResolveFleetAgentByNameAndNick(t *testing.T) {
	all := ListFleetAgents()
	var withNick FleetAgent
	for _, a := range all {
		if a.Nick != "" {
			withNick = a
			break
		}
	}
	if withNick.Name == "" {
		t.Skip("no nicknamed agent in the catalog")
	}

	byName, err := ResolveFleetAgent(withNick.Name)
	if err != nil {
		t.Fatalf("resolve by name %q: %v", withNick.Name, err)
	}
	byNick, err := ResolveFleetAgent(withNick.Nick)
	if err != nil {
		t.Fatalf("resolve by nick %q: %v", withNick.Nick, err)
	}
	if byName.Name != byNick.Name {
		t.Errorf("name and nick disagree: %q vs %q", byName.Name, byNick.Name)
	}
}

// A band selector exists to LEAVE ycode. Resolving one back into ycode is a
// no-op that looks like a successful switch, so a non-ycode tool wins ties.
func TestResolveFleetAgentByBandPrefersLeavingYcode(t *testing.T) {
	for _, sel := range []string{"L4", "l4", "b4", "band:4", "band=4"} {
		a, err := ResolveFleetAgent(sel)
		if err != nil {
			t.Fatalf("resolve %q: %v", sel, err)
		}
		if a.Band < 4 {
			t.Errorf("%q resolved to band %d, want >= 4", sel, a.Band)
		}
		if a.Tool == "ycode" {
			t.Errorf("%q resolved to a ycode agent (%s) — switching should leave ycode", sel, a.Name)
		}
	}
}

// Unlike ResolveFleetModel, which must pass a raw model id through untouched,
// an unresolvable AGENT is an error: silently doing nothing is worse.
func TestResolveFleetAgentFailsLoudly(t *testing.T) {
	for _, sel := range []string{"", "   ", "definitely-not-an-agent"} {
		if _, err := ResolveFleetAgent(sel); err == nil {
			t.Errorf("ResolveFleetAgent(%q) succeeded; want an error", sel)
		}
	}

	_, err := ResolveFleetAgent("definitely-not-an-agent")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The useful reply to a typo names something real.
	if !strings.Contains(err.Error(), "bashy agents list") && !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error gives the reader nowhere to go: %v", err)
	}
}

// A cascade starts on a cheap base and climbs, so reading the BASE model's band
// reports the floor as the ceiling. The agent's own Band is what it serves.
func TestCascadeAgentReportsServedBandNotBaseBand(t *testing.T) {
	var cascade FleetAgent
	for _, a := range ListFleetAgents() {
		if a.Cascade {
			cascade = a
			break
		}
	}
	if cascade.Name == "" {
		t.Skip("no cascade agent in the catalog")
	}
	if cascade.Band == 0 {
		t.Errorf("cascade %s reports band 0 — its served band was not resolved", cascade.Name)
	}

	// The ladder resolver must agree that this is a cascade.
	if _, ok := ResolveCascadeLadder(cascade.Name); !ok {
		t.Errorf("%s is flagged Cascade but ResolveCascadeLadder disagrees", cascade.Name)
	}
}

func TestResolveFleetTool(t *testing.T) {
	name, err := ResolveFleetTool("ycode")
	if err != nil {
		t.Fatalf("ycode should be a known tool: %v", err)
	}
	if name != "ycode" {
		t.Errorf("got %q, want ycode", name)
	}

	_, err = ResolveFleetTool("no-such-tool")
	if err == nil {
		t.Fatal("expected an error for an unknown tool")
	}
	// Naming what IS available is the whole value of the message.
	if !strings.Contains(err.Error(), "known tools:") {
		t.Errorf("error does not list the alternatives: %v", err)
	}
}

func TestYcodeAgentForModelRoundTrips(t *testing.T) {
	var ycodeAgent FleetAgent
	for _, a := range ListFleetAgents() {
		if a.Tool == "ycode" && !a.Cascade {
			ycodeAgent = a
			break
		}
	}
	if ycodeAgent.Name == "" {
		t.Skip("no plain ycode agent in the catalog")
	}

	got, ok := YcodeAgentForModel(ycodeAgent.Model)
	if !ok {
		t.Fatalf("no ycode agent found for model %q", ycodeAgent.Model)
	}
	if got.Tool != "ycode" {
		t.Errorf("reverse lookup returned tool %q, want ycode", got.Tool)
	}

	if _, ok := YcodeAgentForModel("not-a-model"); ok {
		t.Error("an unknown model resolved to an agent")
	}
	if _, ok := YcodeAgentForModel(""); ok {
		t.Error("an empty model resolved to an agent")
	}
}
