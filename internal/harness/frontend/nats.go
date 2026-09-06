package frontend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	gonats "github.com/nats-io/nats.go"
	"github.com/qiangli/ycode/internal/harness/event"
)

type NATSMessage interface {
	Data() []byte
	Reply() string
	Respond([]byte) error
}
type NATSSubscription interface{ Unsubscribe() error }
type NATSConnection interface {
	Endpoint() string
	Subscribe(string, func(NATSMessage)) (NATSSubscription, error)
	Flush(context.Context) error
}

type NATSResponse struct {
	Events []event.Event `json:"events,omitempty"`
	Error  string        `json:"error,omitempty"`
}

// NATSSubscriber binds one compiled subject on one already-authenticated,
// injected connection. Connection establishment and credentials remain owned
// by the host; the endpoint equality check prevents ambient broker selection.
type NATSSubscriber struct {
	network           *Network
	connection        NATSConnection
	endpoint, subject string
}

func NewNATSSubscriber(network *Network, connection NATSConnection) (*NATSSubscriber, error) {
	if network == nil || connection == nil {
		return nil, errors.New("NATS subscriber requires network frontend and connection")
	}
	if network.config.Kind != "nats" {
		return nil, errors.New("NATS subscriber requires a NATS frontend")
	}
	if connection.Endpoint() != network.Endpoint() {
		return nil, fmt.Errorf("NATS connection endpoint %q differs from compiled endpoint %q", connection.Endpoint(), network.Endpoint())
	}
	return &NATSSubscriber{network: network, connection: connection, endpoint: network.Endpoint(), subject: network.Subject()}, nil
}

func (s *NATSSubscriber) Subscribe(ctx context.Context) (NATSSubscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var once sync.Once
	subscription, err := s.connection.Subscribe(s.subject, func(message NATSMessage) {
		response := NATSResponse{}
		events, dispatchErr := s.network.HandleNATS(ctx, message.Data())
		if dispatchErr != nil {
			response.Error = dispatchErr.Error()
		} else {
			response.Events = events
		}
		encoded, marshalErr := json.Marshal(response)
		if marshalErr == nil && message.Reply() != "" {
			_ = message.Respond(encoded)
		}
	})
	if err != nil {
		return nil, err
	}
	if err := s.connection.Flush(ctx); err != nil {
		_ = subscription.Unsubscribe()
		return nil, err
	}
	go func() { <-ctx.Done(); once.Do(func() { _ = subscription.Unsubscribe() }) }()
	return &onceSubscription{subscription: subscription, once: &once}, nil
}

type onceSubscription struct {
	subscription NATSSubscription
	once         *sync.Once
}

func (s *onceSubscription) Unsubscribe() error {
	var err error
	s.once.Do(func() { err = s.subscription.Unsubscribe() })
	return err
}

// GoNATSConnection adapts the repository's existing nats.go mechanism to the
// injectable subscription interface.
type GoNATSConnection struct{ connection *gonats.Conn }

func WrapNATSConnection(connection *gonats.Conn) (*GoNATSConnection, error) {
	if connection == nil {
		return nil, errors.New("NATS connection is nil")
	}
	return &GoNATSConnection{connection: connection}, nil
}
func (c *GoNATSConnection) Endpoint() string { return c.connection.ConnectedUrl() }
func (c *GoNATSConnection) Subscribe(subject string, handler func(NATSMessage)) (NATSSubscription, error) {
	return c.connection.Subscribe(subject, func(message *gonats.Msg) { handler(goNATSMessage{message}) })
}
func (c *GoNATSConnection) Flush(ctx context.Context) error {
	return c.connection.FlushWithContext(ctx)
}

type goNATSMessage struct{ message *gonats.Msg }

func (m goNATSMessage) Data() []byte              { return append([]byte(nil), m.message.Data...) }
func (m goNATSMessage) Reply() string             { return m.message.Reply }
func (m goNATSMessage) Respond(data []byte) error { return m.message.Respond(data) }
