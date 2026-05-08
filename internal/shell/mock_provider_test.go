package shell

import (
	"context"
	"encoding/json"

	"github.com/qiangli/ycode/internal/api"
)

// mockProvider satisfies api.Provider for tests. Each call to Send
// emits a single content_block_delta event whose Delta is a text_delta
// carrying replyText, then closes both channels.
type mockProvider struct {
	replyText string
}

func newMockProvider(reply string) *mockProvider { return &mockProvider{replyText: reply} }

func (m *mockProvider) Kind() api.ProviderKind { return "mock" }

func (m *mockProvider) Send(_ context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent, 2)
	errc := make(chan error, 1)

	delta, _ := json.Marshal(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{Type: "text_delta", Text: m.replyText})

	events <- &api.StreamEvent{Type: "content_block_delta", Delta: delta}
	close(events)
	close(errc)
	return events, errc
}
