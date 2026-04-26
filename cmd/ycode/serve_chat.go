package main

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"

	"github.com/qiangli/ycode/internal/chat"
	"github.com/qiangli/ycode/internal/chat/adapters"
	"github.com/qiangli/ycode/internal/chat/channel"
	"github.com/qiangli/ycode/internal/observability"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/service"
)

// buildChatHub creates and configures a chat Hub component with all
// available channel adapters registered.
func buildChatHub(conn *nats.Conn, cfg *config.ChatConfig, dataDir string, svc service.Service) observability.Component {
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

	// Always enable the agent channel when a service is available.
	if svc != nil {
		if _, ok := hubCfg.Channels[channel.ChannelAgent]; !ok {
			hubCfg.Channels[channel.ChannelAgent] = chat.ChannelConfig{Enabled: true}
		}
	}

	hub := chat.NewHub(conn, hubCfg, dataDir)

	// Register built-in channel adapters.
	hub.RegisterChannel(adapters.NewWebChannel())
	hub.RegisterChannel(adapters.NewTelegramChannel())
	hub.RegisterChannel(adapters.NewDiscordChannel())
	hub.RegisterChannel(adapters.NewWeChatChannel())

	// Register the AI agent adapter if service is available.
	// Pass the hub as StatusBroadcaster so agent progress events
	// reach WebSocket clients in real time.
	if svc != nil {
		hub.RegisterChannel(adapters.NewAgentChannel(svc, hub))

		// Wire model listing through the service so the chat UI can
		// show all available models.
		hub.SetModelLister(func(ctx context.Context) ([]byte, error) {
			models, err := svc.ListModels(ctx)
			if err != nil {
				return nil, err
			}
			return json.Marshal(models)
		})
	}

	return hub
}
