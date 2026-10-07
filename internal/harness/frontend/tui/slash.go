package tui

import (
	"sort"
	"strings"
	"time"
)

// Slash is one human-convenience shorthand. Every slash names the CLI
// subcommand it stands for; the subcommands stay the real surface and a slash
// never does anything its subcommand cannot.
type Slash struct {
	Name    string // "/save"
	Usage   string // "/save [TITLE]"
	Command string // the subcommand it is shorthand for
	Short   string
}

// Slashes is the whole set (Sprint 387 A7): /init /plan /save /resume /model
// plus /help and /quit. There is no registry beyond this table.
var Slashes = []Slash{
	{Name: "/help", Usage: "/help", Command: "--help", Short: "List the slashes and the subcommands they stand for"},
	{Name: "/init", Usage: "/init", Command: "init", Short: "Set up the agent YAML and model for this repository"},
	{Name: "/model", Usage: "/model [NAME]", Command: "model current|use NAME", Short: "Show the session model and the declared ones, or select one"},
	{Name: "/plan", Usage: "/plan [TEXT]", Command: "plan [TEXT]", Short: "Toggle plan mode (no tools), or plan TEXT"},
	{Name: "/quit", Usage: "/quit", Command: "(leave the terminal)", Short: "Leave; the session stays resumable"},
	{Name: "/resume", Usage: "/resume [SESSION]", Command: "continue | resume [SESSION]", Short: "Release a paused turn, else switch to SESSION (default: the latest)"},
	{Name: "/save", Usage: "/save [TITLE]", Command: "session rename SESSION TITLE", Short: "Keep the current session (optionally titled) and print how to resume it"},
}

// parseSlash splits "/name args..." when name is a known slash.
func parseSlash(line string) (Slash, []string, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return Slash{}, nil, false
	}
	for _, s := range Slashes {
		if s.Name == fields[0] {
			return s, fields[1:], true
		}
	}
	return Slash{}, nil, false
}

// completeSlash returns the slash names that start with prefix, sorted.
func completeSlash(prefix string) []string {
	var out []string
	for _, s := range Slashes {
		if strings.HasPrefix(s.Name, prefix) {
			out = append(out, s.Name)
		}
	}
	sort.Strings(out)
	return out
}

func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, v := range values[1:] {
		for !strings.HasPrefix(v, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// settleTimeout bounds the wait for a cancelled turn to record its end.
const settleTimeout = 10 * time.Second
