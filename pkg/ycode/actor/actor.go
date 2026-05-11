// Package actor defines the typed context contract that ycode hosts use to
// attach a User identity to a request context, and that custom tool handlers
// use to read it for authorization decisions.
//
// This is the documented seam between an HTTP auth middleware (which decodes
// a token and produces a User) and a custom tool registered via
// (*ycode.Agent).Registry().Register(...). The middleware calls WithUser to
// stamp the user onto the request context; the tool handler reads it via
// UserFromContext (or the HasRole convenience) before deciding whether to
// proceed.
//
// The package has no runtime behavior beyond context.Value plumbing and is
// shipped without a build tag so third-party code that targets stable
// ycode builds can still depend on the contract.
package actor

import (
	"context"
	"slices"
)

// User identifies the caller for a particular request or session.
//
// Field semantics are host-defined; ycode only carries the value through
// context. Hosts should keep User values immutable after WithUser stamps
// them onto a context.
type User struct {
	// ID is the host-defined stable identifier for the user.
	ID string

	// Email is an optional human-readable identifier. May be empty.
	Email string

	// Roles is the host-defined list of role names assigned to this user
	// (for example "admin", "reader"). Authorization checks compare against
	// these via HasRole.
	Roles []string

	// Extra is a free-form map for host-specific attributes that custom
	// tools may consult. Keep it small — context.Value lookups are not a
	// substitute for a real session store.
	Extra map[string]string
}

type ctxKey struct{}

// WithUser returns a copy of ctx that carries u. Custom tool handlers
// invoked downstream can retrieve it via UserFromContext.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// UserFromContext returns the User stamped on ctx by WithUser, and a
// boolean indicating whether one was present. The zero User is returned
// when no user is attached.
func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

// HasRole reports whether the User on ctx (if any) carries the given role.
// Returns false when no user is attached.
func HasRole(ctx context.Context, role string) bool {
	u, ok := UserFromContext(ctx)
	if !ok {
		return false
	}
	return slices.Contains(u.Roles, role)
}
