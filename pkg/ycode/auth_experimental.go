//go:build experimental

package ycode

import (
	"net/http"

	"github.com/qiangli/ycode/internal/server"
)

// AuthMiddleware is an http.Handler wrapper that authenticates a request and
// stamps a user identity (typically via pkg/ycode/actor.WithUser) onto
// r.Context() before passing to the next handler.
//
// HandlerWithAuth chains this middleware in front of the ycode session API.
// The middleware is the only thing standing between unauthenticated callers
// and /api/sessions — there is no built-in default, by design.
//
// The middleware MUST place its actor.User on the request context using
// actor.WithUser; custom tools registered via (*Agent).Registry().Register
// retrieve the user via actor.UserFromContext on their own handler ctx,
// which is descended from r.Context() and preserves Values across the
// service layer (REST handlers and WebSocket message dispatch alike).
type AuthMiddleware func(http.Handler) http.Handler

// HandlerWithAuth returns Handler() wrapped by mw. The middleware sees every
// HTTP request — REST routes (/api/sessions, /api/messages, etc.), WebSocket
// upgrades (/api/sessions/{id}/ws), and health-check probes.
//
// Pass a no-op middleware for permissive setups; pass a rejecting middleware
// (returning 401 when no token is present) for strict ones. The actor.User
// placed on r.Context() flows all the way to custom tool handlers registered
// via (*Agent).Registry().Register.
func (a *Agent) HandlerWithAuth(mw AuthMiddleware) http.Handler {
	svc, _ := a.ensureService()
	srv := server.New(server.Config{}, svc)
	base := srv.Mux()
	if mw == nil {
		return base
	}
	return mw(base)
}
