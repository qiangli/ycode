package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
)

// The canonical fixture reaches kb only through the bashy.run knowledge and
// persist nodes; the stub boundary returns the frozen golden envelope for
// `bashy kb context` and the turn must surface its blocks as the knowledge
// port of prompt.assembled, provenance included.
func TestTurnKnowledgePortCarriesGoldenEnvelopeIntoPromptAssembled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, err := os.MkdirTemp(".", ".turn-kb-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), raw)
	if err != nil {
		t.Fatal(err)
	}
	backend := &provider.MockBackend{Events: completedWireEvents("kb-informed answer")}
	adapter, err := provider.NewMock(backend)
	if err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(root, "events.jsonl")
	events, err := event.Open(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	ioEngine, err := ioctx.New(ioctx.Config{Document: doc, Events: events, Payloads: payloads, TokenCounter: wordCounter{}, Delivery: &recordingDelivery{}, Redactor: identityRedactor{}, DeadLetter: rejectingDeadLetter{}})
	if err != nil {
		t.Fatal(err)
	}
	memoryEngine, err := memoryStage.New(memoryStage.Config{Document: doc, Events: events, Payloads: payloads, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	bashy := fakeBashy{}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: bashy})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController, Bashy: bashy, Providers: map[string]Provider{"openai": adapter}, Queue: emptyQueue{}})
	if err != nil {
		t.Fatal(err)
	}
	inputBytes := []byte(`{"request":"how do I stop a stuck process"}`)
	inputRef, err := payloads.Put(inputBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(context.Background(), Request{SessionID: "kb-session", RunID: "kb-run", OriginFrontend: "embed", Input: ioctx.CanonicalInput{SchemaVersion: ioctx.SchemaVersion, TriggerRef: "interactive-input", FrontendRef: "embed", Principal: "test", IdempotencyKey: "kb-run", Data: inputBytes, PayloadRef: inputRef}}); err != nil {
		t.Fatal(err)
	}
	replayed, err := event.Replay(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	var assembled json.RawMessage
	var assembledRef string
	requested := []string{}
	for _, item := range replayed {
		switch item.Type {
		case "prompt.assembled":
			assembled = item.Data
			var data struct {
				PayloadRef string `json:"payload_ref"`
			}
			if err := json.Unmarshal(item.Data, &data); err != nil {
				t.Fatal(err)
			}
			assembledRef = data.PayloadRef
		case "bashy.run.requested":
			var data struct {
				PayloadRef string `json:"payload_ref"`
			}
			if err := json.Unmarshal(item.Data, &data); err != nil {
				t.Fatal(err)
			}
			raw, err := payloads.Get(data.PayloadRef)
			if err != nil {
				t.Fatal(err)
			}
			requested = append(requested, string(raw))
		}
	}
	if assembled == nil {
		t.Fatal("turn emitted no prompt.assembled")
	}
	for _, marker := range []string{`"port":"knowledge"`, `"ring":"repo"`, `"form":"page"`, `"ref":"kb:never-pkill-on-an-outpost-host"`, `"ref":"graph:9f2c1a7b0d3e4f56"`} {
		if !strings.Contains(string(assembled), marker) {
			t.Fatalf("prompt.assembled lacks %s: %s", marker, assembled)
		}
	}
	promptRaw, err := payloads.Get(assembledRef)
	if err != nil {
		t.Fatal(err)
	}
	var prompt []ioctx.PromptMessage
	if err := json.Unmarshal(promptRaw, &prompt); err != nil {
		t.Fatal(err)
	}
	knowledge := []ioctx.PromptMessage{}
	for _, item := range prompt {
		if item.Port == ioctx.PortKnowledge {
			knowledge = append(knowledge, item)
		}
	}
	if len(knowledge) != 3 || !strings.Contains(knowledge[0].Content, "never pkill on an outpost host") {
		t.Fatalf("knowledge messages = %#v", knowledge)
	}

	// Template ports are composed as single-quoted literals — never shell
	// variable expansions — so the digest-bound script carries the request
	// text and the session id verbatim.
	joined := strings.Join(requested, "\n")
	if !strings.Contains(joined, "--for 'how do I stop a stuck process'") {
		t.Fatalf("kb context call does not carry the literal task: %s", joined)
	}
	if !strings.Contains(joined, "--rings agent,repo,host") || !strings.Contains(joined, "--forms note,page,relation") || !strings.Contains(joined, "--budget 3000") || !strings.Contains(joined, "--k 8") {
		t.Fatalf("kb context call does not carry the compiled memory policy flags: %s", joined)
	}
	if !strings.Contains(joined, "bashy kb note add --candidate --ring agent --episode 'kb-session'") {
		t.Fatalf("persist call does not bind the session episode literally: %s", joined)
	}
	if strings.Contains(joined, "$YCODE_IN_TASK") || strings.Contains(joined, `\"$`) {
		t.Fatalf("a kb command leaked a shell variable expansion: %s", joined)
	}
}
