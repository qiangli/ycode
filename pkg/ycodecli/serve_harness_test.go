package ycodecli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/qiangli/ycode/internal/harness/frontend"
	public "github.com/qiangli/ycode/pkg/ycode"
)

type serveTestSubscription struct{ once sync.Once }

func (s *serveTestSubscription) Unsubscribe() error { s.once.Do(func() {}); return nil }

type serveTestNATS struct {
	endpoint string
	mu       sync.Mutex
	handler  func(frontend.NATSMessage)
}

func (c *serveTestNATS) Endpoint() string { return c.endpoint }
func (c *serveTestNATS) Subscribe(_ string, handler func(frontend.NATSMessage)) (frontend.NATSSubscription, error) {
	c.mu.Lock()
	c.handler = handler
	c.mu.Unlock()
	return &serveTestSubscription{}, nil
}
func (*serveTestNATS) Flush(context.Context) error { return nil }
func (*serveTestNATS) Close()                      {}

type serveTestMessage struct {
	payload, response []byte
}

func (m *serveTestMessage) Data() []byte { return m.payload }
func (*serveTestMessage) Reply() string  { return "reply" }
func (m *serveTestMessage) Respond(value []byte) error {
	m.response = append([]byte(nil), value...)
	return nil
}

func TestServeCompositionRootBindsConfiguredHTTPWebSocketAndNATS(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YCODE_HTTP_TOKEN", "http-token")
	t.Setenv("YCODE_WS_TOKEN", "ws-token")
	t.Setenv("YCODE_NATS_TOKEN", "nats-token")
	source := filepath.Join("..", "..", "examples", "agent.yaml")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`address: "127.0.0.1:31415"`), []byte(`address: "127.0.0.1:0"`), 1)
	raw = bytes.Replace(raw, []byte(`address: "127.0.0.1:31416"`), []byte(`address: "127.0.0.1:0"`), 1)
	fixtureDir := t.TempDir()
	quotedDir, _ := json.Marshal(fixtureDir)
	raw = bytes.Replace(raw, []byte("    workspace: .\n    readableRoots: [.]\n    writableRoots: [.]"), []byte("    workspace: "+string(quotedDir)+"\n    readableRoots: ["+string(quotedDir)+"]\n    writableRoots: ["+string(quotedDir)+"]"), 1)
	fixture := filepath.Join(fixtureDir, "agent.yaml")
	if err := os.WriteFile(filepath.Join(fixtureDir, "AGENTS.md"), []byte("test harness\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", &applicationProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	fakeNATS := &serveTestNATS{endpoint: "nats://127.0.0.1:4222"}
	started := make(chan struct {
		ref, address string
	}, 3)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- app.serve(ctx, serveDependencies{
			listen:      net.Listen,
			connectNATS: func(string) (natsConnection, error) { return fakeNATS, nil },
			started:     func(ref, address string) { started <- struct{ ref, address string }{ref, address} },
		})
	}()
	addresses := map[string]string{}
	for len(addresses) != 3 {
		select {
		case item := <-started:
			addresses[item.ref] = item.address
		case err := <-done:
			t.Fatalf("serve stopped during startup: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("serve startup timed out")
		}
	}

	wire := `{"operation":"submit","session_id":"network-session","idempotency_key":"network-once","body":{"request":"hello"}}`
	request, _ := http.NewRequest(http.MethodPost, "http://"+addresses["http"], bytes.NewBufferString(wire))
	request.Header.Set("Authorization", "Bearer http-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"type":"output.emitted"`)) {
		t.Fatalf("HTTP status=%d body=%s", response.StatusCode, body)
	}

	header := http.Header{"Authorization": []string{"Bearer ws-token"}}
	connection, _, err := websocket.DefaultDialer.Dial("ws://"+addresses["websocket"], header)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteMessage(websocket.TextMessage, []byte(wire)); err != nil {
		t.Fatal(err)
	}
	seenOutput := false
	for !seenOutput {
		var item struct {
			Type string `json:"type"`
		}
		if err := connection.ReadJSON(&item); err != nil {
			t.Fatal(err)
		}
		seenOutput = item.Type == "output.emitted"
	}
	connection.Close()

	natsWire := `{"operation":"submit","session_id":"nats-session","idempotency_key":"nats-once","credential":"nats-token","body":{"request":"hello"}}`
	message := &serveTestMessage{payload: []byte(natsWire)}
	fakeNATS.mu.Lock()
	handler := fakeNATS.handler
	fakeNATS.mu.Unlock()
	if handler == nil {
		t.Fatal("NATS subscription was not installed")
	}
	handler(message)
	var natsResponse frontend.NATSResponse
	if err := json.Unmarshal(message.response, &natsResponse); err != nil {
		t.Fatal(err)
	}
	if natsResponse.Error != "" || len(natsResponse.Events) == 0 {
		t.Fatalf("NATS response = %#v", natsResponse)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve shutdown timed out")
	}
}
