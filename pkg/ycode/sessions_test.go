package ycode

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/qiangli/ycode/internal/api"
)

func TestHarnessSessionsProjectListTranscriptRenameSearch(t *testing.T) {
	isolateHarnessStores(t)
	backend := newStubProvider(api.ProviderOpenAI)
	var calls atomic.Int32
	backend.streamFunc = func(*api.Request) []*api.StreamEvent {
		value := map[int32]string{1: "alpha answer", 2: "beta answer", 3: "child answer"}[calls.Add(1)]
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": value})
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		return []*api.StreamEvent{{Type: "content_block_delta", Delta: text}, {Type: "message_delta", Delta: stop}}
	}
	harness, err := Load(filepath.Join("..", "..", "examples", "agent.yaml"), WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := harness.Sessions(); err != nil || len(got) != 0 {
		t.Fatalf("empty log: sessions=%v err=%v", got, err)
	}
	runHarnessTurn(t, harness, "sess-alpha", "run-a1", "first prompt about the Parser")
	runHarnessTurn(t, harness, "sess-beta", "run-b1", "beta prompt")

	sessions, err := harness.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != "sess-beta" || sessions[1].ID != "sess-alpha" {
		t.Fatalf("want newest first [beta alpha], got %+v", sessions)
	}
	alpha := sessions[1]
	if alpha.Title != "first prompt about the Parser" || alpha.Runs != 1 || alpha.Committed != 1 || alpha.Head == 0 {
		t.Fatalf("alpha summary: %+v", alpha)
	}

	transcript, err := harness.Transcript("sess-al") // unique prefix
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range transcript {
		texts = append(texts, string(m.Role)+":"+MessageText(m))
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "user:first prompt about the Parser") || !strings.Contains(joined, "assistant:alpha answer") {
		t.Fatalf("transcript: %s", joined)
	}
	if _, err := harness.Session("sess-"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous prefix: err=%v", err)
	}

	if _, err := harness.RenameSession("sess-alpha", "parser work"); err != nil {
		t.Fatal(err)
	}
	if s, _ := harness.Session("sess-alpha"); s.Title != "parser work" {
		t.Fatalf("renamed title: %+v", s)
	}

	matches, err := harness.SearchSessions("parser")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].SessionID != "sess-alpha" || matches[0].Role != "user" || !strings.Contains(matches[0].Excerpt, "Parser") {
		t.Fatalf("search: %+v", matches)
	}

	fork, err := harness.Fork(context.Background(), ForkRequest{ParentSessionID: "sess-alpha", SessionID: "sess-child", RunID: "fork-child", AtSequence: alpha.Head})
	if err != nil {
		t.Fatal(err)
	}
	for range fork {
	}
	child, err := harness.Session("sess-child")
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentID != "sess-alpha" || child.Title != "first prompt about the Parser" {
		t.Fatalf("fork child: %+v", child)
	}
}
