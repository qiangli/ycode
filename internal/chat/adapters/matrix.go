package adapters

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// MatrixChannel bridges messages between the hub and a Matrix homeserver.
// This is a scaffold -- actual Matrix API calls are not yet implemented.
type MatrixChannel struct {
	healthy  atomic.Bool
	logger   *slog.Logger
	accounts []matrixAccount
	inbound  chan<- channel.InboundMessage
	cancel   context.CancelFunc
}

type matrixAccount struct {
	id          string
	homeserver  string
	accessToken string
	roomID      string
}

// MatrixConfig holds Matrix adapter configuration.
type MatrixConfig struct {
	Homeserver  string `json:"homeserver"`
	AccessToken string `json:"access_token"`
	RoomID      string `json:"room_id"`
}

// NewMatrixChannel creates a Matrix channel adapter.
func NewMatrixChannel() *MatrixChannel {
	return &MatrixChannel{
		logger: slog.Default(),
	}
}

func (m *MatrixChannel) ID() channel.ChannelID { return channel.ChannelMatrix }

func (m *MatrixChannel) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		Threads:     true,
		Reactions:   true,
		EditMessage: true,
		Media:       true,
		Markdown:    true,
		MaxTextLen:  65536,
	}
}

func (m *MatrixChannel) Start(ctx context.Context, accounts []channel.AccountConfig, inbound chan<- channel.InboundMessage) error {
	m.inbound = inbound
	_, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	for _, a := range accounts {
		if !a.Enabled {
			continue
		}
		accessToken := a.Config["access_token"]
		if accessToken == "" {
			m.logger.Warn("matrix: account missing access_token", "account", a.AccountID)
			continue
		}
		ma := matrixAccount{
			id:          a.AccountID,
			homeserver:  a.Config["homeserver"],
			accessToken: accessToken,
			roomID:      a.Config["room_id"],
		}
		m.accounts = append(m.accounts, ma)
	}

	if len(m.accounts) > 0 {
		m.healthy.Store(true)
	}
	return nil
}

func (m *MatrixChannel) Stop(_ context.Context) error {
	m.healthy.Store(false)
	if m.cancel != nil {
		m.cancel()
	}
	return nil
}

func (m *MatrixChannel) Healthy() bool { return m.healthy.Load() }

// Send delivers a message to a Matrix room.
// Not yet implemented -- returns ErrNotImplemented.
func (m *MatrixChannel) Send(_ context.Context, _ channel.OutboundTarget, _ channel.OutboundMessage) error {
	return errors.New("matrix: send not implemented")
}
