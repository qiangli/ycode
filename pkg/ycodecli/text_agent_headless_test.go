package ycodecli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	public "github.com/qiangli/ycode/pkg/ycode"
	"github.com/qiangli/yoke/pkg/llmbudget"
)

type capturingProvider struct{ requests []*api.Request }

func (*capturingProvider) Kind() api.ProviderKind { return api.ProviderOpenAI }
func (p *capturingProvider) Send(_ context.Context, request *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	p.requests = append(p.requests, request)
	events := make(chan *api.StreamEvent, 3)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "configured-output"})
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		events <- &api.StreamEvent{Type: "content_block_delta", Delta: text}
		events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
		events <- &api.StreamEvent{Type: "message_stop"}
	}()
	return events, errs
}

// Sprint 379 Story #52 (46f03f676946): a headless genie worker (no human,
// frontend one-shot) was handed a brief that named the exact fix, read the
// repository for 8 minutes, then ended its turn listing four options and
// asking which one to do. The assembled system prompt must tell the model
// to act on the named approach (or report a blocker) instead of ending a turn
// with a question. The swe profile (headless solve runs) names the run
// "a headless run"; the general default profile (dcd2c0e) words it as
// "running headlessly" and keeps an interactive carve-out.
func TestHeadlessOneShotAssembledPromptInstructsActingNotAsking(t *testing.T) {
	for _, profile := range []struct{ name, headless string }{
		{"swe", "headless run"},
		{"general", "running headlessly"},
	} {
		t.Run(profile.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("BASHY_HOME", t.TempDir())
			defer llmbudget.SetDefault(llmbudget.New(llmbudget.Config{StatePath: filepath.Join(t.TempDir(), "meter.json")}))()
			src, err := filepath.Abs(filepath.Join("..", "..", "examples", "genie"))
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), "genie")
			if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
				t.Fatal(err)
			}
			prompt, err := os.ReadFile(filepath.Join(dir, "prompts", "system-"+profile.name+".md"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "prompts", "system.md"), prompt, 0o644); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "agent.yaml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			provider := &capturingProvider{}
			agent, err := OpenTextAgent(path, data, public.WithHarnessProvider("openai", provider))
			if err != nil {
				t.Fatal(err)
			}
			defer agent.Close()
			if _, err := agent.Run(context.Background(), "fix the Windows door hang: Process.Kill it"); err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) == 0 {
				t.Fatal("provider received no request")
			}
			system := provider.requests[0].System
			for _, phrase := range []string{profile.headless, "act instead of asking", "report a concrete blocker"} {
				if !strings.Contains(system, phrase) {
					t.Fatalf("assembled system prompt missing %q:\n%s", phrase, system)
				}
			}
		})
	}
}
