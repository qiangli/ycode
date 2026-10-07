package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Slash is one human-convenience shorthand. Every slash names the CLI
// subcommand it stands for; the subcommands stay the real surface and a slash
// never does anything its subcommand cannot.
type Slash struct {
	Name    string // "/save"
	Usage   string // "/save [TITLE]"
	Command string // the subcommand it is shorthand for
	Short   string
	// Idle slashes change the configuration a turn runs on, so they run
	// only between turns; typed during one they are refused, never steered.
	Idle bool
}

// Slashes is the whole set (Sprint 387 A7): /clear /config /init /plan
// /save /resume /model plus /help and /quit. There is no registry beyond this table.
var Slashes = []Slash{
	{Name: "/help", Usage: "/help", Command: "--help", Short: "List the slashes and the subcommands they stand for"},
	{Name: "/clear", Usage: "/clear", Command: "clear", Short: "Start a clean conversation with this config and model; the old one stays resumable", Idle: true},
	{Name: "/config", Usage: "/config [FILE]", Command: "config source | config use FILE", Short: "Show the effective config and origin, or use FILE (quote a path with spaces)", Idle: true},
	{Name: "/init", Usage: "/init", Command: "init", Short: "Create or use the repo instruction file", Idle: true},
	{Name: "/model", Usage: "/model [NAME]", Command: "model current|use NAME", Short: "Show the session model and the declared ones, or select one"},
	{Name: "/plan", Usage: "/plan [TEXT]", Command: "plan [TEXT]", Short: "Toggle plan mode (no tools), or plan TEXT"},
	{Name: "/quit", Usage: "/quit", Command: "(leave the terminal)", Short: "Leave; the session stays resumable"},
	{Name: "/resume", Usage: "/resume [SESSION]", Command: "continue | resume [SESSION]", Short: "Release a paused turn, else switch to SESSION (default: the latest)"},
	{Name: "/save", Usage: "/save [TITLE]", Command: "session rename SESSION TITLE", Short: "Keep the current session (optionally titled) and print how to resume it"},
}

// parseSlash splits "/name args..." when name is a known slash. Arguments
// split on whitespace, except /config FILE, whose words may be quoted (see
// quotedWords) so a path with spaces stays one FILE; err reports an
// unterminated quote there.
func parseSlash(line string) (s Slash, args []string, ok bool, err error) {
	name, rest := strings.TrimSpace(line), ""
	if i := strings.IndexFunc(name, unicode.IsSpace); i >= 0 {
		name, rest = name[:i], name[i:]
	}
	if !strings.HasPrefix(name, "/") {
		return Slash{}, nil, false, nil
	}
	for _, s := range Slashes {
		if s.Name != name {
			continue
		}
		if s.Name == "/config" {
			args, err = quotedWords(rest)
			return s, args, true, err
		}
		return s, strings.Fields(rest), true, nil
	}
	return Slash{}, nil, false, nil
}

// quotedWords splits text into words on unquoted whitespace. '...' and
// "..." quote everything up to the matching quote literally, and adjacent
// quoted and unquoted parts join into one word. Nothing else is special:
// no escapes, variables, command substitution, globs or ~ — the text is a
// file name, never a shell word to evaluate.
func quotedWords(text string) ([]string, error) {
	var words []string
	var word strings.Builder
	inWord := false
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == '\'' || r == '"':
			end := strings.IndexRune(text[i+1:], r)
			if end < 0 {
				return nil, fmt.Errorf("unterminated %c quote", r)
			}
			word.WriteString(text[i+1 : i+1+end])
			i += end + 2
			inWord = true
			continue
		case unicode.IsSpace(r):
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteString(text[i : i+size])
			inWord = true
		}
		i += size
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
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
