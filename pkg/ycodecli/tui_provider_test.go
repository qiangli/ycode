package ycodecli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/qiangli/ycode/internal/api"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// tuiProvider answers every turn with a fixed text; a request that mentions
// "slow" blocks until its turn is cancelled, so ESC has something to stop.
type tuiProvider struct{ calls atomic.Int32 }

func (*tuiProvider) Kind() api.ProviderKind { return api.ProviderOpenAI }
func (p *tuiProvider) Send(ctx context.Context, request *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	n := p.calls.Add(1)
	events := make(chan *api.StreamEvent, 3)
	errs := make(chan error, 1)
	raw, _ := json.Marshal(request.Messages)
	var last []byte
	if len(request.Messages) > 0 {
		last, _ = json.Marshal(request.Messages[len(request.Messages)-1])
	}
	go func() {
		defer close(events)
		defer close(errs)
		if bytes.Contains(raw, []byte("slow")) && !bytes.Contains(raw, []byte("steer me")) {
			<-ctx.Done()
			errs <- ctx.Err()
			return
		}
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		if bytes.Contains(last, []byte("stream please")) {
			// The first line must reach the screen while the provider is
			// still answering: the rest waits for the test to see it.
			first, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "streamed-line-one\n"})
			events <- &api.StreamEvent{Type: "content_block_delta", Delta: first}
			gate := os.Getenv("YCODE_TUI_STREAM_GATE")
			for gate != "" {
				if _, err := os.Stat(gate); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				case <-time.After(20 * time.Millisecond):
				}
			}
			rest, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "streamed-line-two"})
			events <- &api.StreamEvent{Type: "content_block_delta", Delta: rest}
			events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
			return
		}
		if bytes.Contains(last, []byte("rules?")) {
			// Reports whether the compiled context carried the repository
			// instruction file /init seeds (its template holds the marker).
			text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": fmt.Sprintf("instructions-in-context=%v", strings.Contains(request.System, "GENIE-MARKER-387") || bytes.Contains(raw, []byte("GENIE-MARKER-387")))})
			events <- &api.StreamEvent{Type: "content_block_delta", Delta: text}
			events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
			return
		}
		if bytes.Contains(last, []byte("what did I say?")) {
			// Reports whether anything of a cleared conversation reached
			// this request, and which model the session's selection chose.
			text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": fmt.Sprintf("old-in-request=%v model=%s", bytes.Contains(raw, []byte("CLEAR-OLD-387")) || strings.Contains(request.System, "CLEAR-OLD-387"), request.Model)})
			events <- &api.StreamEvent{Type: "content_block_delta", Delta: text}
			events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
			return
		}
		if bytes.Contains(last, []byte("approve-me")) {
			// A truncating redirect outside the workspace (the workdir's
			// parent) keeps the destroy effect and matches the compiled
			// destructive rule; a workspace overwrite is only a write and is
			// allowed. The engine suspends on hitl.waiting until the human
			// answers.
			toolStop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonToolUse})
			events <- &api.StreamEvent{Type: "content_block_start", ContentBlock: &api.ContentBlock{Type: api.ContentTypeToolUse, ID: "hitl-call", Name: "bashy", Input: json.RawMessage(`{"script":"printf approved-write > ../hitl.txt"}`)}}
			events <- &api.StreamEvent{Type: "content_block_stop"}
			events <- &api.StreamEvent{Type: "message_delta", Delta: toolStop}
			return
		}
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": fmt.Sprintf("stub-answer-%d", n)})
		events <- &api.StreamEvent{Type: "content_block_delta", Delta: text}
		events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
		events <- &api.StreamEvent{Type: "message_stop"}
	}()
	return events, errs
}
