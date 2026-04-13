package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/service"
)

// NATSConfig holds NATS server configuration.
type NATSConfig struct {
	Enabled    bool   `json:"enabled"`
	Port       int    `json:"port"`       // default 4222
	URL        string `json:"url"`        // external NATS URL (if not embedded)
	Embedded   bool   `json:"embedded"`   // start embedded NATS server
	Credential string `json:"credential"` // NATS credentials file
}

// NATSServer manages an embedded NATS server and bridges events between
// the local bus and NATS for remote client connectivity.
type NATSServer struct {
	config  NATSConfig
	service service.Service
	logger  *slog.Logger

	server *natsserver.Server
	conn   *nats.Conn
	bus    *bus.NATSBus

	// Bridge: local bus events → NATS.
	bridgeUnsub func()

	// Input subscription: NATS commands → service.
	inputUnsub func()
}

// NewNATSServer creates a NATS server component.
func NewNATSServer(cfg NATSConfig, svc service.Service) *NATSServer {
	if cfg.Port == 0 {
		cfg.Port = 4222
	}
	return &NATSServer{
		config:  cfg,
		service: svc,
		logger:  slog.Default(),
	}
}

// Start launches the embedded NATS server (or connects to external)
// and sets up event bridging.
func (n *NATSServer) Start(ctx context.Context) error {
	if n.config.Embedded {
		if err := n.startEmbedded(); err != nil {
			return fmt.Errorf("start embedded NATS: %w", err)
		}
	}

	// Connect to NATS.
	url := n.config.URL
	if url == "" {
		url = fmt.Sprintf("nats://127.0.0.1:%d", n.config.Port)
	}

	opts := []nats.Option{
		nats.Name("ycode-server"),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1), // reconnect forever
	}
	if n.config.Credential != "" {
		opts = append(opts, nats.UserCredentials(n.config.Credential))
	}

	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return fmt.Errorf("connect to NATS at %s: %w", url, err)
	}
	n.conn = conn
	n.bus = bus.NewNATSBus(conn)

	// Bridge local bus events to NATS.
	n.startBridge()

	// Listen for NATS input commands from remote clients.
	n.startInputHandler(ctx)

	// Start RPC handler for synchronous operations.
	n.startRPCHandler()

	n.logger.Info("NATS server ready", "url", url, "embedded", n.config.Embedded)
	return nil
}

// startEmbedded launches an in-process NATS server.
func (n *NATSServer) startEmbedded() error {
	opts := &natsserver.Options{
		Port:       n.config.Port,
		Host:       "127.0.0.1",
		NoLog:      true,
		NoSigs:     true,
		MaxPayload: 8 * 1024 * 1024, // 8MB max message
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		return fmt.Errorf("create NATS server: %w", err)
	}
	srv.Start()

	// Wait for server to be ready.
	if !srv.ReadyForConnections(5 * time.Second) {
		srv.Shutdown()
		return fmt.Errorf("NATS server failed to start within 5s")
	}

	n.server = srv
	n.logger.Info("embedded NATS server started", "port", n.config.Port)
	return nil
}

// startBridge subscribes to the local (memory) bus and republishes
// events to NATS so remote clients can receive them.
func (n *NATSServer) startBridge() {
	localBus := n.service.Bus()
	ch, unsub := localBus.Subscribe() // all events
	n.bridgeUnsub = unsub

	go func() {
		for ev := range ch {
			n.bus.Publish(ev)
		}
	}()
}

// startInputHandler subscribes to NATS input commands from remote clients
// and routes them to the service layer.
func (n *NATSServer) startInputHandler(ctx context.Context) {
	ch, unsub := n.bus.SubscribeInput()
	n.inputUnsub = unsub

	go func() {
		for ev := range ch {
			n.handleInput(ctx, ev)
		}
	}()
}

// handleInput dispatches a NATS input event to the service.
func (n *NATSServer) handleInput(ctx context.Context, ev bus.Event) {
	sessionID := ev.SessionID

	switch ev.Type {
	case bus.EventMessageSend:
		var input service.MessageInput
		if err := json.Unmarshal(ev.Data, &input); err != nil {
			n.logger.Error("invalid message.send from NATS", "error", err)
			return
		}
		go func() {
			if err := n.service.SendMessage(ctx, sessionID, input); err != nil {
				n.logger.Error("NATS send message failed", "session", sessionID, "error", err)
			}
		}()

	case bus.EventPermissionRes:
		var resp struct {
			RequestID string `json:"request_id"`
			Allowed   bool   `json:"allowed"`
		}
		if err := json.Unmarshal(ev.Data, &resp); err != nil {
			n.logger.Error("invalid permission.respond from NATS", "error", err)
			return
		}
		if err := n.service.RespondPermission(ctx, resp.RequestID, resp.Allowed); err != nil {
			n.logger.Error("NATS permission respond failed", "error", err)
		}

	case bus.EventTurnCancel:
		if err := n.service.CancelTurn(ctx, sessionID); err != nil {
			n.logger.Error("NATS cancel turn failed", "session", sessionID, "error", err)
		}

	default:
		n.logger.Warn("unknown NATS input type", "type", ev.Type)
	}
}

// Conn returns the NATS connection for direct use.
func (n *NATSServer) Conn() *nats.Conn {
	return n.conn
}

// NATSBus returns the NATS-backed bus.
func (n *NATSServer) NATSBus() *bus.NATSBus {
	return n.bus
}

// startRPCHandler subscribes to NATS request/reply for synchronous operations
// like GetStatus, GetConfig, ListSessions, etc.
func (n *NATSServer) startRPCHandler() {
	subject := "ycode.rpc.>"
	_, err := n.conn.Subscribe(subject, func(msg *nats.Msg) {
		if msg.Reply == "" {
			return
		}

		// Extract operation from subject: ycode.rpc.{operation}
		parts := strings.Split(msg.Subject, ".")
		if len(parts) < 3 {
			return
		}
		op := parts[2]

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var response any
		var opErr error

		switch op {
		case "status":
			response, opErr = n.service.GetStatus(ctx)
		case "config":
			response, opErr = n.service.GetConfig(ctx)
		case "sessions":
			response, opErr = n.service.ListSessions(ctx)
		default:
			opErr = fmt.Errorf("unknown RPC operation: %s", op)
		}

		if opErr != nil {
			data, _ := json.Marshal(map[string]string{"error": opErr.Error()})
			msg.Respond(data)
			return
		}
		data, _ := json.Marshal(response)
		msg.Respond(data)
	})
	if err != nil {
		n.logger.Error("failed to subscribe to NATS RPC", "error", err)
	}
}

// Stop shuts down the NATS server and bridge.
func (n *NATSServer) Stop() error {
	if n.bridgeUnsub != nil {
		n.bridgeUnsub()
	}
	if n.inputUnsub != nil {
		n.inputUnsub()
	}
	if n.bus != nil {
		n.bus.Close()
	}
	if n.conn != nil {
		n.conn.Close()
	}
	if n.server != nil {
		n.server.Shutdown()
		n.logger.Info("embedded NATS server stopped")
	}
	return nil
}
