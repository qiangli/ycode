package frontend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

type testAuth struct{}

func (testAuth) Authenticate(_ context.Context, _ string, _ spec.FrontendAuth, credential string) (string, error) {
	if credential != "Bearer valid" {
		return "", context.Canceled
	}
	return "user-1", nil
}

func TestHTTPAndNATSUseSameCanonicalController(t *testing.T) {
	events := []event.Event{{SchemaVersion: 1, Sequence: 1, Type: "turn.completed"}}
	controller := &fakeController{events: events}
	auth := &spec.FrontendAuth{Mode: "bearer", SecretRef: spec.SecretRef{Provider: "env", Name: "TOKEN"}}
	doc := &spec.Document{Spec: spec.Spec{Frontends: map[string]spec.Frontend{
		"http": {Kind: "http", Listen: &spec.Listen{Network: "tcp", Address: "127.0.0.1:0"}, Auth: auth, HITL: true, Resume: true, Fork: true, Limits: spec.FrontendLimits{MaxInputBytes: 1024, MaxConcurrent: 2}},
		"nats": {Kind: "nats", Endpoint: "nats://127.0.0.1:4222", Subject: "ycode.input", Auth: auth, HITL: true, Resume: true, Fork: true, Limits: spec.FrontendLimits{MaxInputBytes: 1024, MaxConcurrent: 2}},
	}}}
	httpFrontend, err := NewNetwork(doc, "http", controller, testAuth{})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"operation":"submit","session_id":"s","idempotency_key":"once","body":{"request":"hello"}}`
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	httpFrontend.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "turn.completed") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	natsFrontend, err := NewNetwork(doc, "nats", controller, testAuth{})
	if err != nil {
		t.Fatal(err)
	}
	natsBody := map[string]any{"operation": "submit", "session_id": "n", "credential": "Bearer valid", "body": map[string]any{"request": "hello"}}
	encoded, _ := json.Marshal(natsBody)
	got, err := natsFrontend.HandleNATS(context.Background(), encoded)
	if err != nil || len(got) != 1 || got[0].Type != events[0].Type {
		t.Fatalf("events=%#v err=%v", got, err)
	}
	if len(controller.inputs) != 2 || controller.inputs[0].Principal != "user-1" || controller.inputs[1].FrontendRef != "nats" {
		t.Fatalf("inputs=%#v", controller.inputs)
	}
}

func TestNetworkFailsClosedWithoutAuthOrWithUnknownFields(t *testing.T) {
	auth := &spec.FrontendAuth{Mode: "bearer", SecretRef: spec.SecretRef{Provider: "env", Name: "TOKEN"}}
	doc := &spec.Document{Spec: spec.Spec{Frontends: map[string]spec.Frontend{"http": {Kind: "http", Listen: &spec.Listen{Network: "tcp", Address: "127.0.0.1:0"}, Auth: auth, Limits: spec.FrontendLimits{MaxInputBytes: 128}}}}}
	if _, err := NewNetwork(doc, "http", &fakeController{}, nil); err == nil {
		t.Fatal("missing authenticator accepted")
	}
	network, _ := NewNetwork(doc, "http", &fakeController{}, testAuth{})
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"operation":"submit","unknown":true}`))
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	network.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}
