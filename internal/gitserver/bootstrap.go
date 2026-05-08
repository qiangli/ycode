package gitserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	auth_model "code.gitea.io/gitea/models/auth"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/optional"
)

// EnsureAdmin idempotently provisions the admin user against the
// running embedded Gitea. Must be called after Server.Start has
// returned (the database engine and global settings are initialized
// during Start).
//
// If a user with the given name already exists, it is returned
// unchanged — this lets `ycode serve` call EnsureAdmin on every
// startup without churning state.
//
// Pattern mirrors external/gitea/cmd/admin_user_create.go:103
// (`runCreateUser`), without the CLI flag plumbing.
func (s *Server) EnsureAdmin(ctx context.Context, name, email, password string) (*user_model.User, error) {
	if name == "" {
		return nil, errors.New("gitserver.EnsureAdmin: empty name")
	}
	if email == "" {
		return nil, errors.New("gitserver.EnsureAdmin: empty email")
	}
	if password == "" {
		return nil, errors.New("gitserver.EnsureAdmin: empty password")
	}

	if existing, err := user_model.GetUserByName(ctx, name); err == nil && existing != nil {
		return existing, nil
	}

	u := &user_model.User{
		Name:               name,
		Email:              email,
		IsAdmin:            true,
		Type:               user_model.UserTypeIndividual,
		Passwd:             password,
		MustChangePassword: false,
	}
	overwrite := &user_model.CreateUserOverwriteOptions{
		IsActive: optional.Some(true),
	}
	if err := user_model.CreateUser(ctx, u, &user_model.Meta{}, overwrite); err != nil {
		return nil, fmt.Errorf("gitserver.EnsureAdmin: CreateUser: %w", err)
	}
	return u, nil
}

// IssueToken creates a fresh API access token for the named user
// and returns it. Tokens are not retrievable after creation, so
// callers must persist the returned string immediately.
//
// Pattern mirrors external/gitea/cmd/admin_user_generate_access_token.go:47
// (`runGenerateAccessToken`).
func (s *Server) IssueToken(ctx context.Context, userName, tokenName string) (string, error) {
	if userName == "" {
		return "", errors.New("gitserver.IssueToken: empty userName")
	}
	if tokenName == "" {
		return "", errors.New("gitserver.IssueToken: empty tokenName")
	}

	user, err := user_model.GetUserByName(ctx, userName)
	if err != nil {
		return "", fmt.Errorf("gitserver.IssueToken: GetUserByName: %w", err)
	}

	scope, err := auth_model.AccessTokenScope("all").Normalize()
	if err != nil {
		return "", fmt.Errorf("gitserver.IssueToken: scope: %w", err)
	}

	// Token names must be unique per user. If a token with this name
	// already exists, suffix with a short random tag — callers can
	// freely call IssueToken every startup without duplicate-name errors.
	t := &auth_model.AccessToken{Name: tokenName, UID: user.ID, Scope: scope}
	exists, err := auth_model.AccessTokenByNameExists(ctx, t)
	if err != nil {
		return "", fmt.Errorf("gitserver.IssueToken: check existing: %w", err)
	}
	if exists {
		t.Name = tokenName + "-" + randomHex(4)
	}

	if err := auth_model.NewAccessToken(ctx, t); err != nil {
		return "", fmt.Errorf("gitserver.IssueToken: NewAccessToken: %w", err)
	}
	return t.Token, nil
}

// RandomPassword generates a high-entropy password suitable for use
// as the admin's password. The password is never used by humans
// (tooling authenticates via tokens), but Gitea requires CreateUser
// to receive a non-empty value.
func RandomPassword() string {
	return randomHex(16) // 128 bits of entropy, hex-encoded
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is fatal — we don't fall back to weak entropy.
		panic(fmt.Sprintf("gitserver: rand.Read: %v", err))
	}
	return hex.EncodeToString(b)
}
