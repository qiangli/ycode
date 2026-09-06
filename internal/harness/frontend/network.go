package frontend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

type Authenticator interface {
	Authenticate(context.Context, string, spec.FrontendAuth, string) (string, error)
}

type Network struct {
	controller Controller
	ref        string
	config     spec.Frontend
	auth       Authenticator
	sem        chan struct{}
	upgrader   websocket.Upgrader
}

type wireRequest struct {
	Operation      string          `json:"operation"`
	SessionID      string          `json:"session_id"`
	AgentRef       string          `json:"agent_ref,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Credential     string          `json:"credential,omitempty"`
	Token          string          `json:"token,omitempty"`
	AtSequence     uint64          `json:"at_sequence,omitempty"`
	Body           json.RawMessage `json:"body,omitempty"`
}

func NewNetwork(doc *spec.Document, frontendRef string, controller Controller, auth Authenticator) (*Network, error) {
	if doc == nil || controller == nil {
		return nil, errors.New("network frontend requires compiled document and controller")
	}
	configured, ok := doc.Spec.Frontends[frontendRef]
	if !ok {
		return nil, fmt.Errorf("frontend %q is not declared", frontendRef)
	}
	switch configured.Kind {
	case "http", "websocket":
		if configured.Listen == nil || configured.Listen.Network == "" || configured.Listen.Address == "" {
			return nil, fmt.Errorf("frontend %q requires a compiled listen network and address", frontendRef)
		}
	case "nats":
		if configured.Endpoint == "" || configured.Subject == "" {
			return nil, fmt.Errorf("frontend %q requires a compiled endpoint and subject", frontendRef)
		}
	default:
		return nil, fmt.Errorf("frontend %q has non-network kind %q", frontendRef, configured.Kind)
	}
	if configured.Auth == nil {
		return nil, fmt.Errorf("frontend %q requires compiled authentication", frontendRef)
	}
	if auth == nil {
		return nil, fmt.Errorf("frontend %q requires an authenticator", frontendRef)
	}
	maximum := configured.Limits.MaxConcurrent
	if maximum <= 0 {
		maximum = 1
	}
	return &Network{controller: controller, ref: frontendRef, config: configured, auth: auth, sem: make(chan struct{}, maximum), upgrader: websocket.Upgrader{CheckOrigin: sameOrigin}}, nil
}

func (n *Network) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if n.config.Kind == "websocket" {
		n.serveWebSocket(writer, request)
		return
	}
	if n.config.Kind != "http" {
		http.Error(writer, "frontend is not HTTP", http.StatusNotFound)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	principal, err := n.authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !n.acquire(request.Context()) {
		http.Error(writer, "request canceled", http.StatusRequestTimeout)
		return
	}
	defer n.release()
	var wire wireRequest
	reader := io.Reader(request.Body)
	if n.config.Limits.MaxInputBytes > 0 {
		reader = http.MaxBytesReader(writer, request.Body, int64(n.config.Limits.MaxInputBytes))
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		http.Error(writer, "invalid request", http.StatusBadRequest)
		return
	}
	if err := requireJSONEOF(decoder); err != nil {
		http.Error(writer, "invalid request", http.StatusBadRequest)
		return
	}
	stream, err := n.dispatch(request.Context(), principal, wire)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	writer.Header().Set("Content-Type", "application/x-ndjson")
	encoder := json.NewEncoder(writer)
	for {
		select {
		case <-request.Context().Done():
			return
		case item, ok := <-stream:
			if !ok {
				return
			}
			if err := encoder.Encode(item); err != nil {
				return
			}
			if flush, ok := writer.(http.Flusher); ok {
				flush.Flush()
			}
		}
	}
}

func (n *Network) HandleNATS(ctx context.Context, payload []byte) ([]event.Event, error) {
	if n.config.Kind != "nats" {
		return nil, errors.New("frontend is not NATS")
	}
	if n.config.Limits.MaxInputBytes > 0 && len(payload) > n.config.Limits.MaxInputBytes {
		return nil, errors.New("NATS request exceeds compiled input limit")
	}
	var wire wireRequest
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	principal, err := n.authenticate(ctx, wire.Credential)
	if err != nil {
		return nil, err
	}
	if !n.acquire(ctx) {
		return nil, ctx.Err()
	}
	defer n.release()
	stream, err := n.dispatch(ctx, principal, wire)
	if err != nil {
		return nil, err
	}
	var events []event.Event
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case item, ok := <-stream:
			if !ok {
				return events, nil
			}
			events = append(events, item)
		}
	}
}

func (n *Network) serveWebSocket(writer http.ResponseWriter, request *http.Request) {
	principal, err := n.authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := n.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	if n.config.Limits.MaxInputBytes > 0 {
		conn.SetReadLimit(int64(n.config.Limits.MaxInputBytes))
	}
	for {
		var wire wireRequest
		if err := conn.ReadJSON(&wire); err != nil {
			return
		}
		if !n.acquire(request.Context()) {
			return
		}
		stream, dispatchErr := n.dispatch(request.Context(), principal, wire)
		if dispatchErr == nil {
			for {
				select {
				case <-request.Context().Done():
					n.release()
					return
				case item, ok := <-stream:
					if !ok {
						goto streamed
					}
					if err := conn.WriteJSON(item); err != nil {
						n.release()
						return
					}
				}
			}
		}
	streamed:
		n.release()
		if dispatchErr != nil {
			_ = conn.WriteJSON(map[string]any{"type": "frontend.error", "error": dispatchErr.Error()})
		}
	}
}

func (n *Network) dispatch(ctx context.Context, principal string, wire wireRequest) (<-chan event.Event, error) {
	switch wire.Operation {
	case "", "submit":
		if len(wire.Body) == 0 {
			return nil, errors.New("request body is required")
		}
		return n.controller.Submit(ctx, Input{FrontendRef: n.ref, SessionID: wire.SessionID, AgentRef: wire.AgentRef, Principal: principal, IdempotencyKey: wire.IdempotencyKey, Body: append([]byte(nil), wire.Body...)})
	case "resume":
		if !n.config.HITL || !n.config.Resume {
			return nil, errors.New("resume is not enabled")
		}
		return n.controller.Resume(ctx, Resume{FrontendRef: n.ref, SessionID: wire.SessionID, Token: wire.Token, Resolution: append([]byte(nil), wire.Body...)})
	case "fork":
		if !n.config.Fork {
			return nil, errors.New("fork is not enabled")
		}
		return n.controller.Fork(ctx, Fork{FrontendRef: n.ref, SessionID: wire.SessionID, AtSequence: wire.AtSequence})
	default:
		return nil, fmt.Errorf("unsupported operation %q", wire.Operation)
	}
}

func (n *Network) authenticate(ctx context.Context, credential string) (string, error) {
	if n.config.Auth == nil {
		return "local-network", nil
	}
	return n.auth.Authenticate(ctx, n.ref, *n.config.Auth, credential)
}
func (n *Network) acquire(ctx context.Context) bool {
	select {
	case n.sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}
func (n *Network) release() { <-n.sem }
func (n *Network) Listen() (network, address string, ok bool) {
	if n.config.Listen == nil {
		return "", "", false
	}
	return n.config.Listen.Network, n.config.Listen.Address, true
}

// HTTPServer projects the compiled address onto the standard server seam. The
// caller owns listener lifecycle/TLS; it cannot replace the handler or address.
func (n *Network) HTTPServer() (*http.Server, error) {
	if n.config.Kind != "http" && n.config.Kind != "websocket" {
		return nil, errors.New("frontend is not HTTP/WebSocket")
	}
	if n.config.Listen.Network != "tcp" {
		return nil, fmt.Errorf("frontend %q requires unsupported HTTP network %q", n.ref, n.config.Listen.Network)
	}
	return &http.Server{Addr: n.config.Listen.Address, Handler: n}, nil
}
func (n *Network) Endpoint() string { return n.config.Endpoint }
func (n *Network) Subject() string  { return n.config.Subject }
func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request must contain exactly one JSON value")
	}
	return nil
}
func sameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	return origin == "" || origin == "http://"+request.Host || origin == "https://"+request.Host
}
