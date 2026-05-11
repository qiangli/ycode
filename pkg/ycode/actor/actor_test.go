package actor

import (
	"context"
	"testing"
)

func TestWithUserRoundTrip(t *testing.T) {
	ctx := context.Background()
	u := User{ID: "u1", Email: "a@b.c", Roles: []string{"admin"}}
	ctx = WithUser(ctx, u)

	got, ok := UserFromContext(ctx)
	if !ok {
		t.Fatal("UserFromContext returned ok=false after WithUser")
	}
	if got.ID != "u1" || got.Email != "a@b.c" {
		t.Errorf("unexpected user round-trip: %+v", got)
	}
}

func TestUserFromContextEmpty(t *testing.T) {
	_, ok := UserFromContext(context.Background())
	if ok {
		t.Error("expected ok=false on bare context, got true")
	}
}

func TestHasRole(t *testing.T) {
	ctx := WithUser(context.Background(), User{Roles: []string{"reader", "admin"}})

	if !HasRole(ctx, "admin") {
		t.Error("expected HasRole(admin) true")
	}
	if !HasRole(ctx, "reader") {
		t.Error("expected HasRole(reader) true")
	}
	if HasRole(ctx, "writer") {
		t.Error("expected HasRole(writer) false")
	}
	if HasRole(context.Background(), "admin") {
		t.Error("expected HasRole on bare context to be false")
	}
}
