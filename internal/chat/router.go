package chat

import (
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// Router resolves inbound messages to hub rooms and fans out outbound
// messages to all room bindings except the origin.
type Router struct {
	store  *Store
	logger *slog.Logger
}

// NewRouter creates a router backed by the given store.
func NewRouter(store *Store) *Router {
	return &Router{
		store:  store,
		logger: slog.Default(),
	}
}

// ResolveRoom finds or auto-creates a room for an inbound message based on
// its platform binding (channel, account, chat).
func (r *Router) ResolveRoom(channelID channel.ChannelID, accountID, chatID string) (*Room, error) {
	room, err := r.store.FindRoomByBinding(channelID, accountID, chatID)
	if err == nil {
		return room, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("router: lookup binding: %w", err)
	}

	// Auto-create room and binding.
	name := fmt.Sprintf("%s/%s", channelID, chatID)
	room, err = r.store.CreateRoom(name)
	if err != nil {
		return nil, fmt.Errorf("router: create room: %w", err)
	}
	if err := r.store.AddBinding(room.ID, channelID, accountID, chatID); err != nil {
		return nil, fmt.Errorf("router: add binding: %w", err)
	}
	r.logger.Info("router: auto-created room", "room", room.ID, "name", name)
	return room, nil
}

// FanOutTargets returns all bindings for a room except the origin binding,
// so a message can be bridged to all other connected platforms.
func (r *Router) FanOutTargets(roomID string, originChannel channel.ChannelID, originAccountID, originChatID string) ([]*Binding, error) {
	bindings, err := r.store.GetBindings(roomID)
	if err != nil {
		return nil, err
	}
	var targets []*Binding
	for _, b := range bindings {
		if b.ChannelID == originChannel && b.AccountID == originAccountID && b.ChatID == originChatID {
			continue
		}
		targets = append(targets, b)
	}
	return targets, nil
}
