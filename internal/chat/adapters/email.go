package adapters

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// EmailChannel bridges messages between the hub and email via IMAP/SMTP.
// This is a scaffold -- actual IMAP/SMTP operations are not yet implemented.
type EmailChannel struct {
	healthy  atomic.Bool
	logger   *slog.Logger
	accounts []emailAccount
	inbound  chan<- channel.InboundMessage
	cancel   context.CancelFunc
}

type emailAccount struct {
	id           string
	imapHost     string
	imapPort     int
	smtpHost     string
	smtpPort     int
	username     string
	password     string
	pollInterval time.Duration
}

// EmailConfig holds email adapter configuration.
type EmailConfig struct {
	IMAPHost     string        `json:"imap_host"`
	IMAPPort     int           `json:"imap_port"`
	SMTPHost     string        `json:"smtp_host"`
	SMTPPort     int           `json:"smtp_port"`
	Username     string        `json:"username"`
	Password     string        `json:"password"`
	PollInterval time.Duration `json:"poll_interval"`
}

// NewEmailChannel creates an email channel adapter.
func NewEmailChannel() *EmailChannel {
	return &EmailChannel{
		logger: slog.Default(),
	}
}

func (e *EmailChannel) ID() channel.ChannelID { return channel.ChannelEmail }

func (e *EmailChannel) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		Threads:     false,
		Reactions:   false,
		EditMessage: false,
		Media:       true,
		Markdown:    false,
		MaxTextLen:  0, // unlimited
	}
}

func (e *EmailChannel) Start(ctx context.Context, accounts []channel.AccountConfig, inbound chan<- channel.InboundMessage) error {
	e.inbound = inbound
	_, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	for _, a := range accounts {
		if !a.Enabled {
			continue
		}
		username := a.Config["username"]
		if username == "" {
			e.logger.Warn("email: account missing username", "account", a.AccountID)
			continue
		}

		imapPort := 993
		if v := a.Config["imap_port"]; v != "" {
			// Accept string config; parse in production code.
			_ = v
		}
		smtpPort := 587
		if v := a.Config["smtp_port"]; v != "" {
			_ = v
		}

		ea := emailAccount{
			id:           a.AccountID,
			imapHost:     a.Config["imap_host"],
			imapPort:     imapPort,
			smtpHost:     a.Config["smtp_host"],
			smtpPort:     smtpPort,
			username:     username,
			password:     a.Config["password"],
			pollInterval: 60 * time.Second,
		}
		e.accounts = append(e.accounts, ea)
	}

	if len(e.accounts) > 0 {
		e.healthy.Store(true)
	}
	return nil
}

func (e *EmailChannel) Stop(_ context.Context) error {
	e.healthy.Store(false)
	if e.cancel != nil {
		e.cancel()
	}
	return nil
}

func (e *EmailChannel) Healthy() bool { return e.healthy.Load() }

// Send delivers a message via SMTP.
// Not yet implemented -- returns ErrNotImplemented.
func (e *EmailChannel) Send(_ context.Context, _ channel.OutboundTarget, _ channel.OutboundMessage) error {
	return errors.New("email: send not implemented")
}
