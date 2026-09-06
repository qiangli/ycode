package frontend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func networkDocument() *spec.Document {
	auth := &spec.FrontendAuth{Mode: "bearer", SecretRef: spec.SecretRef{Provider: "env", Name: "TOKEN"}}
	return &spec.Document{Spec: spec.Spec{Runtime: spec.Runtime{Workspace: "/compiled/workspace"}, Frontends: map[string]spec.Frontend{
		"websocket": {Kind: "websocket", Listen: &spec.Listen{Network: "tcp", Address: "127.0.0.1:31416"}, Auth: auth, HITL: true, Resume: true, Limits: spec.FrontendLimits{MaxInputBytes: 1024, MaxConcurrent: 2}},
		"nats":      {Kind: "nats", Endpoint: "nats://compiled:4222", Subject: "ycode.input", Auth: auth, HITL: true, Resume: true, Limits: spec.FrontendLimits{MaxInputBytes: 1024, MaxConcurrent: 2}},
	}}}
}

func TestWebSocketAuthCanonicalParityAndCompiledAddress(t *testing.T) {
	doc := networkDocument()
	events := []event.Event{{SchemaVersion: 1, Sequence: 1, Type: "text.delta"}, {SchemaVersion: 1, Sequence: 2, Type: "turn.completed"}}
	controller := &fakeController{events: events}
	network, err := NewNetwork(doc, "websocket", controller, testAuth{})
	if err != nil {
		t.Fatal(err)
	}
	if protocol, address, ok := network.Listen(); !ok || protocol != "tcp" || address != "127.0.0.1:31416" {
		t.Fatalf("listen=(%q,%q,%v)", protocol, address, ok)
	}
	configuredServer, err := network.HTTPServer()
	if err != nil || configuredServer.Addr != "127.0.0.1:31416" || configuredServer.Handler != network {
		t.Fatalf("configured server=%#v err=%v", configuredServer, err)
	}
	server := httptest.NewServer(network)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	_, response, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated dial response=%v err=%v", response, err)
	}
	header := http.Header{"Authorization": []string{"Bearer valid"}}
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	wire := wireRequest{Operation: "submit", SessionID: "session", AgentRef: "coder", IdempotencyKey: "once", Body: json.RawMessage(`{"request":"hello"}`)}
	if err := conn.WriteJSON(wire); err != nil {
		t.Fatal(err)
	}
	for _, want := range events {
		var got event.Event
		if err := conn.ReadJSON(&got); err != nil {
			t.Fatal(err)
		}
		if got.Type != want.Type || got.Sequence != want.Sequence {
			t.Fatalf("event=%#v want=%#v", got, want)
		}
	}
	if len(controller.inputs) != 1 || controller.inputs[0].FrontendRef != "websocket" || controller.inputs[0].Principal != "user-1" || controller.inputs[0].AgentRef != "coder" {
		t.Fatalf("input=%#v", controller.inputs)
	}
}

type cancelNetworkController struct {
	canceled chan struct{}
	once     sync.Once
}

func (c *cancelNetworkController) Submit(ctx context.Context, _ Input) (<-chan event.Event, error) {
	stream := make(chan event.Event)
	go func() {
		defer close(stream)
		stream <- event.Event{SchemaVersion: 1, Sequence: 1, Type: "text.delta"}
		<-ctx.Done()
		c.once.Do(func() { close(c.canceled) })
	}()
	return stream, nil
}
func (*cancelNetworkController) Resume(context.Context, Resume) (<-chan event.Event, error) {
	return nil, errors.New("unused")
}
func (*cancelNetworkController) Fork(context.Context, Fork) (<-chan event.Event, error) {
	return nil, errors.New("unused")
}

func TestWebSocketStreamingCancellationReachesController(t *testing.T) {
	doc := networkDocument()
	controller := &cancelNetworkController{canceled: make(chan struct{})}
	network, _ := NewNetwork(doc, "websocket", controller, testAuth{})
	var cancel context.CancelFunc
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, stop := context.WithCancel(r.Context())
		cancel = stop
		network.ServeHTTP(w, r.WithContext(ctx))
	}))
	defer server.Close()
	header := http.Header{"Authorization": []string{"Bearer valid"}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(wireRequest{SessionID: "session", Body: json.RawMessage(`{"request":"hello"}`)}); err != nil {
		t.Fatal(err)
	}
	var first event.Event
	if err := conn.ReadJSON(&first); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-controller.canceled:
	case <-time.After(time.Second):
		t.Fatal("websocket cancellation did not reach controller")
	}
}

type fakeNATSSubscription struct {
	mu           sync.Mutex
	unsubscribed bool
}

func (s *fakeNATSSubscription) Unsubscribe() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unsubscribed = true
	return nil
}

type fakeNATSConnection struct {
	endpoint, subject string
	handler           func(NATSMessage)
	subscription      *fakeNATSSubscription
	flushed           bool
}

func (c *fakeNATSConnection) Endpoint() string { return c.endpoint }
func (c *fakeNATSConnection) Subscribe(subject string, handler func(NATSMessage)) (NATSSubscription, error) {
	c.subject = subject
	c.handler = handler
	c.subscription = &fakeNATSSubscription{}
	return c.subscription, nil
}
func (c *fakeNATSConnection) Flush(context.Context) error { c.flushed = true; return nil }

type fakeNATSMessage struct {
	payload  []byte
	reply    string
	response []byte
}

func (m *fakeNATSMessage) Data() []byte  { return m.payload }
func (m *fakeNATSMessage) Reply() string { return m.reply }
func (m *fakeNATSMessage) Respond(value []byte) error {
	m.response = append([]byte(nil), value...)
	return nil
}

func TestNATSSubscriptionUsesCompiledBoundaryAndCanonicalEvents(t *testing.T) {
	doc := networkDocument()
	events := []event.Event{{SchemaVersion: 1, Sequence: 1, Type: "text.delta"}, {SchemaVersion: 1, Sequence: 2, Type: "turn.completed"}}
	controller := &fakeController{events: events}
	network, _ := NewNetwork(doc, "nats", controller, testAuth{})
	connection := &fakeNATSConnection{endpoint: "nats://compiled:4222"}
	subscriber, err := NewNATSSubscriber(network, connection)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	subscription, err := subscriber.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if connection.subject != "ycode.input" || !connection.flushed {
		t.Fatalf("subject=%q flushed=%v", connection.subject, connection.flushed)
	}
	payload, _ := json.Marshal(wireRequest{SessionID: "session", AgentRef: "coder", Credential: "Bearer valid", IdempotencyKey: "once", Body: json.RawMessage(`{"request":"hello"}`)})
	message := &fakeNATSMessage{payload: payload, reply: "reply.inbox"}
	connection.handler(message)
	var response NATSResponse
	if err := json.Unmarshal(message.response, &response); err != nil {
		t.Fatal(err)
	}
	if !reflectEvents(response.Events, events) || response.Error != "" {
		t.Fatalf("response=%#v", response)
	}
	if len(controller.inputs) != 1 || controller.inputs[0].FrontendRef != "nats" || controller.inputs[0].Principal != "user-1" {
		t.Fatalf("inputs=%#v", controller.inputs)
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for {
		connection.subscription.mu.Lock()
		done := connection.subscription.unsubscribed
		connection.subscription.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscription not canceled")
		}
		time.Sleep(time.Millisecond)
	}
	if err := subscription.Unsubscribe(); err != nil {
		t.Fatal(err)
	}
}

func TestNATSRejectsAmbientEndpointAndWorkspaceOverride(t *testing.T) {
	doc := networkDocument()
	network, _ := NewNetwork(doc, "nats", &fakeController{}, testAuth{})
	if _, err := NewNATSSubscriber(network, &fakeNATSConnection{endpoint: "nats://ambient:4222"}); err == nil {
		t.Fatal("ambient endpoint accepted")
	}
	payload := []byte(`{"session_id":"s","credential":"Bearer valid","workspace":"/tmp","body":{"request":"hello"}}`)
	if _, err := network.HandleNATS(context.Background(), payload); err == nil {
		t.Fatal("request workspace override accepted")
	}
}

func reflectEvents(a, b []event.Event) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].Sequence != b[i].Sequence {
			return false
		}
	}
	return true
}
