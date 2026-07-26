package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/api"
)

// SwitchRequest is a request to hand this session to another agent or tool.
//
// It is a struct rather than a bare selector because the CONTEXT CHOICE is
// part of the request, and the choice is the feature: switching through ycode
// is only worth doing because the conversation travels with you.
type SwitchRequest struct {
	Verb  string // "agent" or "tool", for messages
	Agent string // agent name, nickname, or band selector
	Tool  string // tool name, using that tool's own configured defaults
	Fresh bool   // opt OUT of carrying context

	// Takeover hands the TERMINAL over instead of proxying: you leave ycode,
	// drive the other tool's own full-screen UI, and return when it exits.
	// The default is to attach, which keeps you here — see App.Attach.
	Takeover bool
}

// DetachRequest asks to end the attached session. A distinct type so the
// handler cannot be called with a half-filled SwitchRequest.
type DetachRequest struct{}

// SelectorOrDefault renders whatever the user named, for error messages that
// suggest the equivalent direct command.
func (r SwitchRequest) SelectorOrDefault() string {
	if r.Agent != "" {
		return r.Agent
	}
	if r.Tool != "" {
		return r.Tool
	}
	return "<agent>"
}

// parseSwitchArgs splits a selector from the recognized flags. Only --fresh is
// understood; anything else unknown is an error rather than being passed
// through, so a typo cannot silently become part of the selector.
func parseSwitchArgs(args string) (selector string, fresh, takeover bool, err error) {
	for _, f := range strings.Fields(args) {
		switch {
		case f == "--fresh" || f == "--no-context":
			fresh = true
		case f == "--takeover":
			takeover = true
		case strings.HasPrefix(f, "-"):
			return "", false, false, fmt.Errorf("unknown flag %q (supported: --fresh, --takeover)", f)
		case selector == "":
			selector = f
		default:
			return "", false, false, fmt.Errorf("unexpected argument %q", f)
		}
	}
	return selector, fresh, takeover, nil
}

// registerSwitchCommands adds /agent and /tool — the same switch, differing
// only in what is selected: a full agent (tool:model) or a tool running with
// its own preconfigured model.
func registerSwitchCommands(r *Registry, deps *RuntimeDeps) {
	r.Register(&Spec{
		Name:        "agent",
		Description: "Switch to another agent, carrying this conversation",
		Usage:       "/agent [name|nick|L3] [--fresh] [--takeover]",
		Category:    "session",
		Examples: []string{
			"switch to codex",
			"hand this to claude",
			"use a stronger agent",
			"let opencode take over",
			"which agents are available",
		},
		Handler: switchHandler(deps, "agent"),
	})

	r.Register(&Spec{
		Name:        "detach",
		Description: "Return to ycode from an attached agent",
		Category:    "session",
		Examples: []string{
			"come back to ycode",
			"stop using codex",
			"detach from the agent",
			"go back",
		},
		Handler: func(ctx context.Context, args string) (string, error) {
			if deps.Detacher == nil {
				return "", fmt.Errorf("nothing to detach from")
			}
			return deps.Detacher()
		},
	})

	r.Register(&Spec{
		Name:        "tool",
		Description: "Switch to another agentic tool with its own settings",
		Usage:       "/tool [name] [--fresh] [--takeover]",
		Category:    "session",
		Examples: []string{
			"switch to the codex CLI",
			"run claude instead",
			"what tools can I switch to",
		},
		Handler: switchHandler(deps, "tool"),
	})
}

func switchHandler(deps *RuntimeDeps, verb string) HandlerFunc {
	return func(ctx context.Context, args string) (string, error) {
		selector, fresh, takeover, err := parseSwitchArgs(args)
		if err != nil {
			return "", err
		}
		// No selector: show what is available. This is pure and always safe,
		// so it works even where switching itself does not (shell, --print).
		if selector == "" {
			return listSwitchTargets(verb), nil
		}
		if deps.AgentSwitcher == nil {
			return "", fmt.Errorf("switching is only available in the interactive TUI; "+
				"from here run: bashy chat --%s %s -i", verb, selector)
		}

		req := SwitchRequest{Verb: verb, Fresh: fresh, Takeover: takeover}
		if verb == "tool" {
			req.Tool = selector
		} else {
			req.Agent = selector
		}
		return deps.AgentSwitcher(ctx, req)
	}
}

// listSwitchTargets renders the roster, grouped by band so the strongest are
// visible first.
func listSwitchTargets(verb string) string {
	var b strings.Builder
	if verb == "tool" {
		b.WriteString("Tools you can switch to (each uses its own configured model):\n\n")
		seen := map[string]bool{}
		for _, a := range api.ListFleetAgents() {
			if a.Tool == "" || seen[a.Tool] {
				continue
			}
			seen[a.Tool] = true
			fmt.Fprintf(&b, "  %s\n", a.Tool)
		}
		b.WriteString("\nUsage: /tool <name> [--fresh] [--takeover]\n")
		return b.String()
	}

	b.WriteString("Agents you can switch to:\n\n")
	band := -1
	for _, a := range api.ListFleetAgents() {
		if a.Band != band {
			band = a.Band
			fmt.Fprintf(&b, "  L%d\n", band)
		}
		nick := a.Nick
		if nick != "" {
			nick = " (" + nick + ")"
		}
		fmt.Fprintf(&b, "    %-26s %s%s\n", a.Name, a.Binding, nick)
	}
	b.WriteString("\nUsage: /agent <name|nick|L3> [--fresh] [--takeover]\n" +
		"  The conversation is carried across unless --fresh is given.\n" +
		"  You stay in ycode and its replies appear here; --takeover instead hands\n" +
		"  over the terminal to the agent's own UI until it exits.\n")
	return b.String()
}
