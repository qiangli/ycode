package chat

import (
	"net/http"

	chatweb "github.com/qiangli/ycode/internal/chat/web"
)

// chatWebHandler returns the embedded static file handler for the chat web UI.
func chatWebHandler() http.Handler {
	return chatweb.Handler()
}
