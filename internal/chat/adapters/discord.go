package adapters

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/bwmarrin/discordgo"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// DiscordChannel bridges messages between the hub and Discord.
type DiscordChannel struct {
	healthy  atomic.Bool
	logger   *slog.Logger
	sessions []*discordSession
	inbound  chan<- channel.InboundMessage
	cancel   context.CancelFunc
}

type discordSession struct {
	accountID string
	session   *discordgo.Session
	guildID   string // optional: filter to a specific guild
}

// NewDiscordChannel creates a Discord channel adapter.
func NewDiscordChannel() *DiscordChannel {
	return &DiscordChannel{
		logger: slog.Default(),
	}
}

func (d *DiscordChannel) ID() channel.ChannelID { return channel.ChannelDiscord }

func (d *DiscordChannel) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		Threads:     true,
		Reactions:   true,
		EditMessage: true,
		Media:       true,
		Markdown:    true,
		MaxTextLen:  2000,
	}
}

func (d *DiscordChannel) Start(ctx context.Context, accounts []channel.AccountConfig, inbound chan<- channel.InboundMessage) error {
	d.inbound = inbound
	_, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	for _, a := range accounts {
		if !a.Enabled {
			continue
		}
		token := a.Config["bot_token"]
		if token == "" {
			d.logger.Warn("discord: account missing bot_token", "account", a.AccountID)
			continue
		}

		sess, err := discordgo.New("Bot " + token)
		if err != nil {
			d.logger.Error("discord: create session", "error", err, "account", a.AccountID)
			continue
		}

		ds := &discordSession{
			accountID: a.AccountID,
			session:   sess,
			guildID:   a.Config["guild_id"],
		}

		// Register message handler.
		sess.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
			d.handleMessage(ds, m)
		})

		// Only request message content intent.
		sess.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentMessageContent

		if err := sess.Open(); err != nil {
			d.logger.Error("discord: open session", "error", err, "account", a.AccountID)
			continue
		}

		d.sessions = append(d.sessions, ds)
		d.logger.Info("discord: connected", "account", a.AccountID)
	}

	if len(d.sessions) > 0 {
		d.healthy.Store(true)
	}
	return nil
}

func (d *DiscordChannel) Stop(_ context.Context) error {
	d.healthy.Store(false)
	if d.cancel != nil {
		d.cancel()
	}
	for _, ds := range d.sessions {
		ds.session.Close()
	}
	d.sessions = nil
	return nil
}

func (d *DiscordChannel) Healthy() bool { return d.healthy.Load() }

// Send delivers a message to a Discord channel.
func (d *DiscordChannel) Send(_ context.Context, target channel.OutboundTarget, msg channel.OutboundMessage) error {
	var ds *discordSession
	for _, s := range d.sessions {
		if s.accountID == target.AccountID || target.AccountID == "" {
			ds = s
			break
		}
	}
	if ds == nil {
		return fmt.Errorf("discord: no session for account %q", target.AccountID)
	}

	_, err := ds.session.ChannelMessageSend(target.ChatID, msg.Content.Text)
	return err
}

func (d *DiscordChannel) handleMessage(ds *discordSession, m *discordgo.MessageCreate) {
	// Ignore bot's own messages.
	if m.Author.Bot {
		return
	}

	// Filter by guild if configured.
	if ds.guildID != "" && m.GuildID != ds.guildID {
		return
	}

	senderName := m.Author.Username
	if m.Member != nil && m.Member.Nick != "" {
		senderName = m.Member.Nick
	}

	threadID := ""
	if m.Thread != nil {
		threadID = m.Thread.ID
	}

	d.inbound <- channel.InboundMessage{
		ChannelID:  channel.ChannelDiscord,
		AccountID:  ds.accountID,
		SenderID:   m.Author.ID,
		SenderName: senderName,
		Content:    channel.MessageContent{Text: m.Content},
		PlatformID: m.ID,
		ChatID:     m.ChannelID,
		ThreadID:   threadID,
		Timestamp:  m.Timestamp,
	}
}
