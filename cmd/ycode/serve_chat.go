package main

import (
	"github.com/nats-io/nats.go"

	"github.com/qiangli/ycode/internal/chat"
	"github.com/qiangli/ycode/internal/chat/adapters"
	"github.com/qiangli/ycode/internal/chat/channel"
	"github.com/qiangli/ycode/internal/observability"
	"github.com/qiangli/ycode/internal/runtime/config"
)

// buildChatHub creates and configures a chat Hub component with all
// available channel adapters registered.
func buildChatHub(conn *nats.Conn, cfg *config.ChatConfig, dataDir string) observability.Component {
	// Convert config types.
	hubCfg := &chat.HubConfig{
		Enabled:  cfg.Enabled,
		Channels: make(map[channel.ChannelID]chat.ChannelConfig),
	}
	for id, chCfg := range cfg.Channels {
		var accounts []channel.AccountConfig
		for _, a := range chCfg.Accounts {
			accounts = append(accounts, channel.AccountConfig{
				AccountID: a.ID,
				Enabled:   a.Enabled,
				Config:    a.Config,
			})
		}
		hubCfg.Channels[channel.ChannelID(id)] = chat.ChannelConfig{
			Enabled:  chCfg.Enabled,
			Accounts: accounts,
		}
	}

	// Always enable the web channel.
	if _, ok := hubCfg.Channels[channel.ChannelWeb]; !ok {
		hubCfg.Channels[channel.ChannelWeb] = chat.ChannelConfig{Enabled: true}
	}

	hub := chat.NewHub(conn, hubCfg, dataDir)

	// Register built-in channel adapters.
	hub.RegisterChannel(adapters.NewWebChannel())
	hub.RegisterChannel(adapters.NewTelegramChannel())
	hub.RegisterChannel(adapters.NewDiscordChannel())
	hub.RegisterChannel(adapters.NewWeChatChannel())

	return hub
}
