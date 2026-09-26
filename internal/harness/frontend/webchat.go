package frontend

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/qiangli/ycode/internal/harness/event"
)

//go:embed webchat/index.html
var webChatPage []byte

// PayloadReader resolves a content-addressed payload. A controller that
// implements it lets a network frontend return the delivered output text with
// output.emitted, so a client needs no second request for the answer.
type PayloadReader interface {
	Payload(ref string) ([]byte, error)
}

// outputEvent is output.emitted with the text the sink delivered to this, the
// originating, frontend.
type outputEvent struct {
	event.Event
	Output string `json:"output"`
}

// wireEvent is what a network client receives for item.
func (n *Network) wireEvent(item event.Event) any {
	reader, ok := n.controller.(PayloadReader)
	if !ok || item.Type != "output.emitted" {
		return item
	}
	var body struct {
		Deliveries []struct {
			PayloadRef string `json:"payload_ref"`
		} `json:"deliveries"`
	}
	if json.Unmarshal(item.Data, &body) != nil || len(body.Deliveries) != 1 || body.Deliveries[0].PayloadRef == "" {
		return item
	}
	content, err := reader.Payload(body.Deliveries[0].PayloadRef)
	if err != nil {
		return item
	}
	return outputEvent{Event: item, Output: string(content)}
}

// serveUI serves the frontend's built-in page. The page holds no secret: it
// reads the bearer token from the URL fragment and calls the POST API.
func (n *Network) serveUI(writer http.ResponseWriter, request *http.Request) bool {
	if n.config.UI != "chat" || request.Method != http.MethodGet {
		return false
	}
	if request.URL.Path != "/" && request.URL.Path != "/index.html" {
		http.NotFound(writer, request)
		return true
	}
	header := writer.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
	_, _ = writer.Write(webChatPage)
	return true
}
