package swarm

import (
	"encoding/json"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// AgentMessagePayload is the event data for inter-agent messaging.
type AgentMessagePayload struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Message string `json:"message"`
}

// SendToAgent publishes an inter-agent message via the event bus.
func SendToAgent(b bus.Bus, fromID, toID, message string) {
	payload := AgentMessagePayload{
		From:    fromID,
		To:      toID,
		Message: message,
	}
	data, _ := json.Marshal(payload)
	b.Publish(bus.Event{
		Type:      bus.EventAgentMessage,
		Timestamp: time.Now(),
		Data:      data,
	})
}

// ReceiveFromAgent subscribes to agent messages and waits for one addressed to myID.
// Returns the message and true, or empty and false on timeout.
func ReceiveFromAgent(b bus.Bus, myID string, timeout time.Duration) (AgentMessagePayload, bool) {
	ch, unsub := b.Subscribe(bus.EventAgentMessage)
	defer unsub()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case ev := <-ch:
			var payload AgentMessagePayload
			if err := json.Unmarshal(ev.Data, &payload); err != nil {
				continue
			}
			if payload.To == myID {
				return payload, true
			}
		case <-timer.C:
			return AgentMessagePayload{}, false
		}
	}
}
