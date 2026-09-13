package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
)

// The testdata envelope is the frozen cross-repo golden
// (docs/kb-context-envelope.json in the umbrella); it must stay
// byte-identical, so this test reads it rather than restating it.
func goldenEnvelope(t *testing.T) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "kb-context-envelope.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestKnowledgeMessagesProjectGoldenEnvelopeWithProvenance(t *testing.T) {
	messages, err := KnowledgeMessages(goldenEnvelope(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %#v", messages)
	}
	first := messages[0]
	if first.Port != ioctx.PortKnowledge || first.Role != "system" || first.Ring != "repo" || first.Form != "page" || first.Ref != "kb:never-pkill-on-an-outpost-host" {
		t.Fatalf("first message = %#v", first)
	}
	if first.Content == "" || messages[1].Ring != "agent" || messages[2].Form != "relation" || messages[2].Ref != "graph:9f2c1a7b0d3e4f56" {
		t.Fatalf("provenance drifted: %#v", messages)
	}
}

func TestKnowledgeMessagesFailClosedOnMalformedEnvelopes(t *testing.T) {
	cases := map[string]string{
		"missing-version":     `{"budget":{"limit":1,"used":0},"abstained":false,"rings":[],"blocks":[]}`,
		"wrong-version":       `{"context_version":2,"budget":{"limit":1,"used":0},"abstained":false,"rings":[],"blocks":[]}`,
		"missing-budget":      `{"context_version":1,"abstained":false,"rings":[],"blocks":[]}`,
		"missing-abstained":   `{"context_version":1,"budget":{"limit":1,"used":0},"rings":[],"blocks":[]}`,
		"abstained-has-block": `{"context_version":1,"budget":{"limit":1,"used":0},"abstained":true,"rings":[],"blocks":[{"ring":"repo","form":"page","ref":"kb:x","tokens":1,"text":"x"}]}`,
		"block-missing-ref":   `{"context_version":1,"budget":{"limit":1,"used":0},"abstained":false,"rings":[],"blocks":[{"ring":"repo","form":"page","tokens":1,"text":"x"}]}`,
		"not-an-envelope":     `"kb-note-skipped"`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var value any
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				t.Fatal(err)
			}
			if _, err := KnowledgeMessages(value); err == nil {
				t.Fatal("malformed envelope was accepted")
			}
		})
	}
}

func TestKnowledgeMessagesAcceptAbstainedEnvelopeAsEmpty(t *testing.T) {
	var value any
	if err := json.Unmarshal([]byte(`{"context_version":1,"for":"","budget":{"limit":0,"used":0},"abstained":true,"rings":[],"blocks":[]}`), &value); err != nil {
		t.Fatal(err)
	}
	messages, err := KnowledgeMessages(value)
	if err != nil || len(messages) != 0 {
		t.Fatalf("messages = %#v, %v", messages, err)
	}
}
