package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// TelegramChannel bridges messages between the hub and Telegram via the Bot API.
// Uses the Telegram Bot API (HTTPS) for simplicity and permissive licensing.
// A future upgrade could use gotd/td (MIT) for full MTProto user-account puppeting.
type TelegramChannel struct {
	healthy  atomic.Bool
	logger   *slog.Logger
	accounts []telegramAccount
	inbound  chan<- channel.InboundMessage
	cancel   context.CancelFunc
}

type telegramAccount struct {
	id    string
	token string
}

// NewTelegramChannel creates a Telegram channel adapter.
func NewTelegramChannel() *TelegramChannel {
	return &TelegramChannel{
		logger: slog.Default(),
	}
}

func (t *TelegramChannel) ID() channel.ChannelID { return channel.ChannelTelegram }

func (t *TelegramChannel) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		Threads:     true,
		Reactions:   true,
		EditMessage: true,
		Media:       true,
		Markdown:    true,
		MaxTextLen:  4096,
	}
}

func (t *TelegramChannel) Start(ctx context.Context, accounts []channel.AccountConfig, inbound chan<- channel.InboundMessage) error {
	t.inbound = inbound
	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	for _, a := range accounts {
		if !a.Enabled {
			continue
		}
		token := a.Config["bot_token"]
		if token == "" {
			t.logger.Warn("telegram: account missing bot_token", "account", a.AccountID)
			continue
		}
		ta := telegramAccount{id: a.AccountID, token: token}
		t.accounts = append(t.accounts, ta)
		go t.poll(ctx, ta)
	}

	if len(t.accounts) > 0 {
		t.healthy.Store(true)
	}
	return nil
}

func (t *TelegramChannel) Stop(_ context.Context) error {
	t.healthy.Store(false)
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}

func (t *TelegramChannel) Healthy() bool { return t.healthy.Load() }

// Send delivers a message to a Telegram chat via the Bot API.
func (t *TelegramChannel) Send(_ context.Context, target channel.OutboundTarget, msg channel.OutboundMessage) error {
	var account *telegramAccount
	for i := range t.accounts {
		if t.accounts[i].id == target.AccountID || target.AccountID == "" {
			account = &t.accounts[i]
			break
		}
	}
	if account == nil {
		return fmt.Errorf("telegram: no account found for %q", target.AccountID)
	}

	return t.sendMessage(account.token, target.ChatID, msg.Content.Text)
}

// poll uses Telegram Bot API long-polling (getUpdates) to receive messages.
func (t *TelegramChannel) poll(ctx context.Context, account telegramAccount) {
	offset := 0
	client := &http.Client{Timeout: 35 * time.Second}
	base := fmt.Sprintf("https://api.telegram.org/bot%s", account.token)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		url := fmt.Sprintf("%s/getUpdates?offset=%d&timeout=30", base, offset)
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.logger.Error("telegram: poll error", "error", err, "account", account.id)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result struct {
			OK     bool `json:"ok"`
			Result []struct {
				UpdateID int `json:"update_id"`
				Message  *struct {
					MessageID int `json:"message_id"`
					Chat      struct {
						ID    int64  `json:"id"`
						Title string `json:"title"`
						Type  string `json:"type"`
					} `json:"chat"`
					From *struct {
						ID        int64  `json:"id"`
						FirstName string `json:"first_name"`
						LastName  string `json:"last_name"`
						Username  string `json:"username"`
					} `json:"from"`
					Text string `json:"text"`
					Date int64  `json:"date"`
				} `json:"message"`
			} `json:"result"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			t.logger.Error("telegram: parse error", "error", err)
			continue
		}

		for _, update := range result.Result {
			offset = update.UpdateID + 1
			msg := update.Message
			if msg == nil || msg.Text == "" {
				continue
			}

			senderName := ""
			senderID := ""
			if msg.From != nil {
				senderName = msg.From.FirstName
				if msg.From.LastName != "" {
					senderName += " " + msg.From.LastName
				}
				senderID = fmt.Sprintf("%d", msg.From.ID)
			}

			t.inbound <- channel.InboundMessage{
				ChannelID:  channel.ChannelTelegram,
				AccountID:  account.id,
				SenderID:   senderID,
				SenderName: senderName,
				Content:    channel.MessageContent{Text: msg.Text},
				PlatformID: fmt.Sprintf("%d", msg.MessageID),
				ChatID:     fmt.Sprintf("%d", msg.Chat.ID),
				Timestamp:  time.Unix(msg.Date, 0),
			}
		}
	}
}

// sendMessage sends a text message via the Bot API.
func (t *TelegramChannel) sendMessage(token, chatID, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]string{
		"chat_id": chatID,
		"text":    text,
	}
	data, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", jsonReader(data))
	if err != nil {
		return fmt.Errorf("telegram: send: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram: send: status %d", resp.StatusCode)
	}
	return nil
}
