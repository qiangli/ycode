package adapters

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// SlackChannel bridges messages between the hub and Slack using Socket Mode.
// This is a scaffold -- actual Slack API calls are not yet implemented.
type SlackChannel struct {
	healthy  atomic.Bool
	logger   *slog.Logger
	accounts []slackAccount
	inbound  chan<- channel.InboundMessage
	cancel   context.CancelFunc
}

type slackAccount struct {
	id        string
	appToken  string // xapp-... token for Socket Mode
	botToken  string // xoxb-... token for Bot API
	channelID string // default channel to send messages
}

// SlackConfig holds Slack adapter configuration.
type SlackConfig struct {
	AppToken  string `json:"app_token"`
	BotToken  string `json:"bot_token"`
	ChannelID string `json:"channel_id"`
}

// NewSlackChannel creates a Slack channel adapter.
func NewSlackChannel() *SlackChannel {
	return &SlackChannel{
		logger: slog.Default(),
	}
}

func (s *SlackChannel) ID() channel.ChannelID { return channel.ChannelSlack }

func (s *SlackChannel) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		Threads:     true,
		Reactions:   true,
		EditMessage: true,
		Media:       true,
		Markdown:    true,
		MaxTextLen:  40000,
	}
}

func (s *SlackChannel) Start(ctx context.Context, accounts []channel.AccountConfig, inbound chan<- channel.InboundMessage) error {
	s.inbound = inbound
	_, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	for _, a := range accounts {
		if !a.Enabled {
			continue
		}
		botToken := a.Config["bot_token"]
		if botToken == "" {
			s.logger.Warn("slack: account missing bot_token", "account", a.AccountID)
			continue
		}
		sa := slackAccount{
			id:        a.AccountID,
			appToken:  a.Config["app_token"],
			botToken:  botToken,
			channelID: a.Config["channel_id"],
		}
		s.accounts = append(s.accounts, sa)
	}

	if len(s.accounts) > 0 {
		s.healthy.Store(true)
	}
	return nil
}

func (s *SlackChannel) Stop(_ context.Context) error {
	s.healthy.Store(false)
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *SlackChannel) Healthy() bool { return s.healthy.Load() }

// Send delivers a message to a Slack channel.
// Not yet implemented -- returns ErrNotImplemented.
func (s *SlackChannel) Send(_ context.Context, _ channel.OutboundTarget, _ channel.OutboundMessage) error {
	return errors.New("slack: send not implemented")
}
