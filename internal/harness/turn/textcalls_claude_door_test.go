package turn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/provider"
	sessionStage "github.com/qiangli/ycode/internal/harness/stages/session"
)

// Sprint 531 story #60: genie on a claude model through the model door
// (cligw) answered with its bashy call written as text, the call never ran,
// and the delegate exited 0 with the work undone. The fixtures are real
// claude-via-door replies recorded by this harness; each must recover into
// exactly the one call the model meant.

func readTextCallFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "textcalls", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func recoveredScript(t *testing.T, body string) (string, textCallRepairs) {
	t.Helper()
	calls, repairs, ok := recoverTextToolCallsDetail(body)
	if !ok {
		t.Fatalf("recoverTextToolCallsDetail refused %q", body)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want exactly one", calls)
	}
	script, _ := calls[0].(map[string]any)["input"].(map[string]any)["script"].(string)
	return script, repairs
}

// Prose, then the envelope, whose heredoc commit message carries raw double
// quotes the model did not escape (`POSIX ">3"`, `the name "localedef"`).
func TestRecoverClaudeDoorProseEnvelopeWithUnescapedQuotes(t *testing.T) {
	script, repairs := recoveredScript(t, readTextCallFixture(t, "claude-door-prose-envelope-unescaped-quote.txt"))
	for _, want := range []string{`errors occurred (POSIX ">3"), including`, `the name "localedef" is still`, "git commit -q -F - <<'EOF'", "\nrun_checked"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script lacks %q:\n%s", want, script)
		}
	}
	if !repairs.Quotes || repairs.Closers {
		t.Fatalf("repairs = %+v, want only the quote repair", repairs)
	}
}

// The envelope whose call entry is never closed (S329 shape) still recovers.
func TestRecoverClaudeDoorUnclosedEntry(t *testing.T) {
	script, repairs := recoveredScript(t, readTextCallFixture(t, "claude-door-unclosed-entry.txt"))
	if !strings.HasPrefix(script, `@effects("read,write,exec")`) || !strings.HasSuffix(script, "\nrun_checked") {
		t.Fatalf("script = %q", script)
	}
	if !repairs.Closers {
		t.Fatalf("repairs = %+v, want the closer repair", repairs)
	}
}

// A transcript-style call line, `<glyph> bashy({...})`, followed by a result
// the model made up (`<glyph> exit 0 ... OK`). The call is what runs; the
// invented result is never treated as one.
func TestRecoverClaudeDoorCallMarkup(t *testing.T) {
	script, repairs := recoveredScript(t, readTextCallFixture(t, "claude-door-call-markup.txt"))
	if !strings.Contains(script, "python3 - <<'EOF'") || !strings.HasSuffix(script, "\nrun_checked") {
		t.Fatalf("script = %q", script)
	}
	if strings.Contains(script, "Ran 11 tests") {
		t.Fatalf("script swallowed the invented result: %q", script)
	}
	if repairs != (textCallRepairs{}) {
		t.Fatalf("repairs = %+v, want none", repairs)
	}
}

func TestRecoverTextToolCallsProseAndMarkupShapes(t *testing.T) {
	cases := map[string]string{
		"prose then envelope":    "Now I will run the tests.\n\n" + `{"tool_calls":[{"name":"bashy","arguments":{"script":"sh test_slug.sh"}}]}`,
		"prose then single call": "Running it.\n" + `{"name":"bashy","arguments":{"script":"sh test_slug.sh"}}`,
		"markup alone":           `bashy({"script":"sh test_slug.sh"})`,
		"markup after prose":     "Let me run it.\n\n🔀 bashy({\"script\":\"sh test_slug.sh\"})\n💻 exit 0\nPASS",
		"markup spaced":          "- bashy( {\"script\":\"sh test_slug.sh\",\"timeout_ms\":5000} )",
		"envelope bad quotes":    `{"tool_calls":[{"name":"bashy","arguments":{"script":"echo "hi" > f"}}]}`,
	}
	for name, body := range cases {
		calls, ok := recoverTextToolCalls(body)
		if !ok || len(calls) != 1 {
			t.Fatalf("%s: calls = %#v, ok = %v, want one recovered call", name, calls, ok)
		}
	}
}

func TestRecoverTextToolCallsProseAndMarkupRefusals(t *testing.T) {
	cases := map[string]string{
		// A trailing envelope must end the reply; prose after it is a
		// discussion of the call, not the call.
		"envelope then prose": "Here: " + `{"tool_calls":[{"name":"bashy","arguments":{"script":"ls"}}]}` + " and then I will check.",
		// Two envelopes are ambiguous.
		"two envelopes": "First " + `{"tool_calls":[{"name":"bashy","arguments":{"script":"one"}}]}` + "\nthen " + `{"tool_calls":[{"name":"bashy","arguments":{"script":"two"}}]}`,
		// A call mentioned inside a sentence is prose.
		"inline mention": `You can run bashy({"script":"ls"}) to list files.`,
		// Two call lines are ambiguous.
		"two markups": "bashy({\"script\":\"one\"})\nbashy({\"script\":\"two\"})",
		// The markup must close with its parenthesis.
		"markup unclosed":    `bashy({"script":"ls"}`,
		"markup no script":   `bashy({"cmd":"ls"})`,
		"markup unknown key": `bashy({"script":"ls","shell":"zsh"})`,
		"markup other tool":  `shell({"script":"ls"})`,
		// A stray quote whose repair still does not parse refuses.
		"quote then comma": `{"tool_calls":[{"name":"bashy","arguments":{"script":"echo "a", b"}}]}`,
		// Truncation is never guessed at, quotes or not.
		"truncated quotes": `{"tool_calls":[{"name":"bashy","arguments":{"script":"echo "hi`,
	}
	for name, body := range cases {
		if calls, ok := recoverTextToolCalls(body); ok {
			t.Fatalf("%s: recovered %#v, want refusal", name, calls)
		}
	}
}

func TestRepairUnescapedQuotesWellFormedIsNoRepair(t *testing.T) {
	if fixed, changed := repairUnescapedQuotes(`{"script":"echo \"hi\"","timeout_ms":5}`); changed {
		t.Fatalf("well-formed document was repaired: %q", fixed)
	}
	fixed, changed := repairUnescapedQuotes(`{"script":"echo "hi" > f"}`)
	if !changed || fixed != `{"script":"echo \"hi\" > f"}` {
		t.Fatalf("repairUnescapedQuotes = %q, %v", fixed, changed)
	}
}

// The story's failure end to end: the claude reply is text, and the turn
// must run the call it carries through the Bashy boundary instead of
// delivering the text as the answer, journaling the repair it needed.
func TestTextToolCallRecoveryExecutesClaudeDoorReplies(t *testing.T) {
	for _, tc := range []struct {
		fixture, want string
		quotes        bool
	}{
		{"claude-door-prose-envelope-unescaped-quote.txt", `the name "localedef" is still`, true},
		{"claude-door-call-markup.txt", "python3 - <<'EOF'", false},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			scratchStores(t)
			root, err := os.MkdirTemp(".", ".turn-test-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			final := "DONE: ran it."
			fake, bashy, delivery, eventPath := runScriptedRecoveryTurn(t, root, "claude-door", [][]provider.Event{
				completedText(readTextCallFixture(t, tc.fixture)),
				completedText(final),
			})
			if got := fake.callCount(); got != 2 {
				t.Fatalf("model calls = %d, want the text call plus DONE", got)
			}
			matched := 0
			for _, script := range bashy.executed() {
				if strings.Contains(script, tc.want) {
					matched++
				}
			}
			if matched != 1 {
				t.Fatalf("recovered script executed %d times, want exactly once", matched)
			}
			if got := countEventsOfType(t, eventPath, sessionStage.ToolCallCompleted); got != 1 {
				t.Fatalf("tool.call.completed events = %d, want the one recovered call", got)
			}
			if delivery.last != final {
				t.Fatalf("delivered %q, want the DONE completion", delivery.last)
			}
			replayed, err := event.Replay(eventPath)
			if err != nil {
				t.Fatal(err)
			}
			recovered := 0
			for _, item := range replayed {
				if item.Type != textToolCallRecoveredEvent {
					continue
				}
				var data map[string]any
				if err := json.Unmarshal(item.Data, &data); err != nil {
					t.Fatal(err)
				}
				if data["call_count"] != float64(1) || (data["repaired_quotes"] == true) != tc.quotes {
					t.Fatalf("recovery event = %#v, want call_count 1, repaired_quotes %v", data, tc.quotes)
				}
				recovered++
			}
			if recovered != 1 {
				t.Fatalf("recovery events = %d, want exactly one", recovered)
			}
		})
	}
}
