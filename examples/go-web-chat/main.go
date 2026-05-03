// A web chat server using ycode as a Go module with local Ollama models.
// ycode runs as the full agentic agent with tools, memory, and all features.
//
// Usage:
//
//	go run -tags "sqlite,sqlite_unlock_notify,bindata" .
//
// Then open http://localhost:8080 in your browser.
// Requires Ollama running locally with a model pulled (e.g. ollama pull qwen3:8b).
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/qiangli/ycode/pkg/ycode"
)

//go:embed index.html
var staticFS embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	// Create a ycode agent using local Ollama.
	// This gives you the full agent: tools (bash, file ops, search), memory, and more.
	model := "qwen3:8b"
	agent, err := ycode.NewAgent(
		ycode.WithModel(model),
		ycode.WithOllama(""),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v\nMake sure Ollama is running: ollama serve", err)
	}

	// WebSocket chat endpoint — streams agent responses.
	http.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		handleChat(conn, agent)
	})

	// Serve the embedded HTML UI.
	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		data, _ := staticFS.ReadFile("index.html")
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
	})

	addr := ":8080"
	fmt.Printf("Chat server at http://localhost%s\n", addr)
	fmt.Printf("Model: %s (full agentic mode with tools + memory)\n", model)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func handleChat(conn *websocket.Conn, agent *ycode.Agent) {
	var mu sync.Mutex
	writeJSON := func(v any) {
		mu.Lock()
		defer mu.Unlock()
		conn.WriteJSON(v)
	}

	for {
		// Read user message.
		var msg struct {
			Text string `json:"text"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		// Run the full agentic loop — tools, memory, multi-turn reasoning.
		err := agent.Chat(context.Background(), msg.Text, func(ev ycode.Event) {
			var data map[string]any
			json.Unmarshal(ev.Data, &data)

			switch ev.Type {
			case "text.delta":
				if text, ok := data["text"].(string); ok {
					writeJSON(map[string]string{"type": "delta", "text": text})
				}
			case "tool_use.start":
				tool, _ := data["tool"].(string)
				detail, _ := data["detail"].(string)
				writeJSON(map[string]string{"type": "tool", "tool": tool, "detail": detail})
			case "turn.complete":
				writeJSON(map[string]string{"type": "done"})
			case "turn.error":
				errText, _ := data["error"].(string)
				writeJSON(map[string]string{"type": "error", "text": errText})
			}
		})
		if err != nil {
			writeJSON(map[string]string{"type": "error", "text": err.Error()})
		}
	}
}
