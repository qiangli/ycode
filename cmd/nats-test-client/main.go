// nats-test-client is a simple NATS client that connects to the ycode
// NATS server and sends/receives messages for testing.
//
// Usage:
//
//	go run ./cmd/nats-test-client -session <id> -message "hello"
//	go run ./cmd/nats-test-client -session <id> -listen
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/qiangli/ycode/internal/bus"
)

func main() {
	var (
		natsURL   string
		sessionID string
		message   string
		listen    bool
	)

	flag.StringVar(&natsURL, "url", "nats://127.0.0.1:4222", "NATS server URL")
	flag.StringVar(&sessionID, "session", "", "Session ID to interact with")
	flag.StringVar(&message, "message", "", "Message to send")
	flag.BoolVar(&listen, "listen", false, "Listen for events on the session")
	flag.Parse()

	if sessionID == "" {
		log.Fatal("--session is required")
	}

	// Connect to NATS.
	conn, err := nats.Connect(natsURL,
		nats.Name("ycode-test-client"),
		nats.ReconnectWait(2*time.Second),
		nats.MaxReconnects(5),
	)
	if err != nil {
		log.Fatalf("connect to NATS: %v", err)
	}
	defer conn.Close()

	fmt.Printf("Connected to NATS at %s\n", natsURL)

	if listen || message == "" {
		// Subscribe to all events for this session.
		subject := bus.SessionEventsSubject(sessionID)
		fmt.Printf("Subscribing to: %s\n\n", subject)

		sub, err := conn.Subscribe(subject, func(msg *nats.Msg) {
			var event bus.Event
			if err := json.Unmarshal(msg.Data, &event); err != nil {
				fmt.Printf("  [error] unmarshal: %v\n", err)
				return
			}
			printEvent(event)
		})
		if err != nil {
			log.Fatalf("subscribe: %v", err)
		}
		defer sub.Unsubscribe()

		if message != "" {
			// Send message and listen.
			sendMessage(conn, sessionID, message)
		} else {
			fmt.Println("Listening for events... (Ctrl+C to exit)")
		}

		// Wait for signal.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fmt.Println("\nExiting.")
		return
	}

	// Send-only mode.
	sendMessage(conn, sessionID, message)
	fmt.Println("Message sent. Use -listen to see events.")
}

func sendMessage(conn *nats.Conn, sessionID, text string) {
	event := bus.Event{
		ID:        bus.NextEventID(),
		Type:      bus.EventMessageSend,
		SessionID: sessionID,
		Timestamp: time.Now(),
		Data:      mustJSON(map[string]string{"text": text}),
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Fatalf("marshal event: %v", err)
	}

	subject := bus.InputSubject(sessionID)
	if err := conn.Publish(subject, data); err != nil {
		log.Fatalf("publish: %v", err)
	}
	conn.Flush()

	fmt.Printf("Sent message to %s: %q\n", subject, text)
}

func printEvent(event bus.Event) {
	// Parse data for display.
	var dataMap map[string]any
	_ = json.Unmarshal(event.Data, &dataMap)

	switch event.Type {
	case bus.EventTurnStart:
		fmt.Printf("  [turn.start] Turn started\n")
	case bus.EventTextDelta:
		text, _ := dataMap["text"].(string)
		fmt.Print(text)
	case bus.EventThinkingDelta:
		text, _ := dataMap["text"].(string)
		fmt.Printf("  [thinking] %s", text)
	case bus.EventToolUseStart:
		tool, _ := dataMap["tool"].(string)
		fmt.Printf("\n  [tool] Starting: %s\n", tool)
	case bus.EventToolProgress:
		tool, _ := dataMap["tool"].(string)
		status, _ := dataMap["status"].(string)
		fmt.Printf("  [tool] %s: %s\n", tool, status)
	case bus.EventToolResult:
		status, _ := dataMap["status"].(string)
		fmt.Printf("  [tool.result] %s\n", status)
	case bus.EventTurnComplete:
		status, _ := dataMap["status"].(string)
		fmt.Printf("\n  [turn.complete] %s\n", status)
	case bus.EventTurnError:
		errMsg, _ := dataMap["error"].(string)
		fmt.Printf("\n  [turn.error] %s\n", errMsg)
	case bus.EventUsageUpdate:
		fmt.Printf("  [usage] input=%v output=%v\n", dataMap["input_tokens"], dataMap["output_tokens"])
	default:
		fmt.Printf("  [%s] %s\n", event.Type, string(event.Data))
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
