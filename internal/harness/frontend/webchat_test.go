package frontend

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

type payloadController struct {
	fakeController
	payloads map[string]string
}

func (c *payloadController) Payload(ref string) ([]byte, error) {
	if text, ok := c.payloads[ref]; ok {
		return []byte(text), nil
	}
	return nil, errors.New("no payload")
}

func webChatDoc(ui string) *spec.Document {
	auth := &spec.FrontendAuth{Mode: "bearer", SecretRef: spec.SecretRef{Provider: "env", Name: "TOKEN"}}
	return &spec.Document{Spec: spec.Spec{Frontends: map[string]spec.Frontend{
		"web": {Kind: "http", UI: ui, Listen: &spec.Listen{Network: "tcp", Address: "127.0.0.1:0"}, Auth: auth, Limits: spec.FrontendLimits{MaxInputBytes: 1024, MaxConcurrent: 1}},
	}}}
}

func TestWebChatUIServesPageWithoutTokenAndAPIStillNeedsIt(t *testing.T) {
	network, err := NewNetwork(webChatDoc("chat"), "web", &fakeController{}, testAuth{})
	if err != nil {
		t.Fatal(err)
	}
	page := httptest.NewRecorder()
	network.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "<title>Agent chat</title>") || !strings.Contains(page.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatalf("page: %d %q", page.Code, page.Header())
	}
	missing := httptest.NewRecorder()
	network.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/secrets", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("other GET path: %d", missing.Code)
	}
	unauthorized := httptest.NewRecorder()
	network.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"operation":"submit","body":{"request":"x"}}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("POST without token: %d", unauthorized.Code)
	}

	plain, _ := NewNetwork(webChatDoc(""), "web", &fakeController{}, testAuth{})
	noUI := httptest.NewRecorder()
	plain.ServeHTTP(noUI, httptest.NewRequest(http.MethodGet, "/", nil))
	if noUI.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET on a frontend without ui: %d", noUI.Code)
	}
}

func TestNetworkOutputEmittedCarriesDeliveredText(t *testing.T) {
	data, _ := json.Marshal(map[string]any{"deliveries": []map[string]string{{"sink_ref": "primary", "payload_ref": "sha256:abc"}}})
	controller := &payloadController{
		fakeController: fakeController{events: []event.Event{{SchemaVersion: 1, Sequence: 1, Type: "bashy.requested"}, {SchemaVersion: 1, Sequence: 2, Type: "output.emitted", Data: data}}},
		payloads:       map[string]string{"sha256:abc": "the answer"},
	}
	network, err := NewNetwork(webChatDoc("chat"), "web", controller, testAuth{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"operation":"submit","session_id":"s","body":{"request":"hi"}}`))
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	network.ServeHTTP(response, request)
	lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("stream: %s", response.Body.String())
	}
	var first, last map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &first)
	_ = json.Unmarshal([]byte(lines[1]), &last)
	if _, has := first["output"]; has || last["type"] != "output.emitted" || last["output"] != "the answer" || last["sequence"] != float64(2) {
		t.Fatalf("events: %v / %v", first, last)
	}
}
