package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultClientID is the OAuth client ID for Claude CLI tools.
	DefaultClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	// DefaultAuthorizeURL is the OAuth authorization endpoint.
	DefaultAuthorizeURL = "https://platform.claude.com/oauth/authorize"
	// DefaultTokenURL is the OAuth token exchange endpoint.
	DefaultTokenURL = "https://platform.claude.com/v1/oauth/token"
	// DefaultCallbackPort is the local port for the OAuth callback server.
	DefaultCallbackPort = 4545
	// DefaultScopes are the OAuth scopes requested during login.
	defaultScopes = "user:profile user:inference user:sessions:claude_code"
)

// Token represents a stored OAuth token set.
type Token struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken,omitempty"`
	ExpiresAt    int64    `json:"expiresAt,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

// IsExpired checks if the token has expired.
func (t *Token) IsExpired() bool {
	if t.ExpiresAt == 0 {
		return false
	}
	return time.Now().Unix() > t.ExpiresAt
}

// CallbackParams holds the parsed OAuth callback query parameters.
type CallbackParams struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

// PKCEFlow implements the PKCE OAuth authorization code flow.
type PKCEFlow struct {
	ClientID     string
	AuthURL      string
	TokenURL     string
	CallbackPort int
	Scopes       []string

	verifier string
	state    string
}

// NewPKCEFlow creates a new PKCE flow with default Claude OAuth settings.
func NewPKCEFlow() *PKCEFlow {
	return &PKCEFlow{
		ClientID:     DefaultClientID,
		AuthURL:      DefaultAuthorizeURL,
		TokenURL:     DefaultTokenURL,
		CallbackPort: DefaultCallbackPort,
		Scopes:       strings.Split(defaultScopes, " "),
	}
}

// RedirectURI returns the local callback URI.
func (f *PKCEFlow) RedirectURI() string {
	return fmt.Sprintf("http://localhost:%d/callback", f.CallbackPort)
}

// AuthorizationURL generates the authorization URL with PKCE challenge and state.
func (f *PKCEFlow) AuthorizationURL() (string, error) {
	// Generate PKCE verifier.
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	f.verifier = base64.RawURLEncoding.EncodeToString(b)

	// Generate challenge (S256).
	h := sha256.Sum256([]byte(f.verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	// Generate state for CSRF protection.
	sb := make([]byte, 32)
	if _, err := rand.Read(sb); err != nil {
		return "", err
	}
	f.state = base64.RawURLEncoding.EncodeToString(sb)

	u, err := url.Parse(f.AuthURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", f.ClientID)
	q.Set("redirect_uri", f.RedirectURI())
	q.Set("response_type", "code")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", f.state)
	if len(f.Scopes) > 0 {
		q.Set("scope", strings.Join(f.Scopes, " "))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// WaitForCallback starts a local HTTP server and waits for the OAuth callback.
func (f *PKCEFlow) WaitForCallback() (*CallbackParams, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", f.CallbackPort))
	if err != nil {
		return nil, fmt.Errorf("listen on port %d: %w", f.CallbackPort, err)
	}
	defer listener.Close()

	conn, err := listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("accept callback connection: %w", err)
	}
	defer conn.Close()

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read callback request: %w", err)
	}

	request := string(buf[:n])
	lines := strings.SplitN(request, "\n", 2)
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty callback request")
	}

	parts := strings.Fields(lines[0])
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed callback request line")
	}
	target := parts[1]

	params, err := parseCallbackTarget(target)
	if err != nil {
		return nil, err
	}

	// Send response to browser.
	body := "Claude OAuth login succeeded. You can close this window."
	if params.Error != "" {
		body = "Claude OAuth login failed. You can close this window."
	}
	response := fmt.Sprintf(
		"HTTP/1.1 200 OK\r\ncontent-type: text/plain; charset=utf-8\r\ncontent-length: %d\r\nconnection: close\r\n\r\n%s",
		len(body), body,
	)
	conn.Write([]byte(response))

	return params, nil
}

// Exchange trades an authorization code for tokens.
func (f *PKCEFlow) Exchange(ctx context.Context, code string) (*Token, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {f.RedirectURI()},
		"client_id":     {f.ClientID},
		"code_verifier": {f.verifier},
		"state":         {f.state},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.TokenURL,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string   `json:"access_token"`
		RefreshToken string   `json:"refresh_token"`
		ExpiresIn    int64    `json:"expires_in"`
		ExpiresAt    int64    `json:"expires_at"`
		Scopes       []string `json:"scopes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	expiresAt := tokenResp.ExpiresAt
	if expiresAt == 0 && tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + tokenResp.ExpiresIn
	}

	return &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
		Scopes:       tokenResp.Scopes,
	}, nil
}

// ValidateState checks the returned state matches the original.
func (f *PKCEFlow) ValidateState(returnedState string) error {
	if returnedState != f.state {
		return fmt.Errorf("oauth state mismatch")
	}
	return nil
}

// RefreshToken exchanges a refresh token for a new access token.
func RefreshToken(ctx context.Context, tokenURL, clientID, refreshToken string) (*Token, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string   `json:"access_token"`
		RefreshToken string   `json:"refresh_token"`
		ExpiresIn    int64    `json:"expires_in"`
		ExpiresAt    int64    `json:"expires_at"`
		Scopes       []string `json:"scopes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	expiresAt := tokenResp.ExpiresAt
	if expiresAt == 0 && tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + tokenResp.ExpiresIn
	}

	return &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
		Scopes:       tokenResp.Scopes,
	}, nil
}

// credentialsPath returns the path to the credentials.json file.
func credentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ycode", "credentials.json"), nil
}

// readCredentialsRoot reads and parses the credentials.json file.
func readCredentialsRoot() (map[string]json.RawMessage, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]json.RawMessage), nil
		}
		return nil, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return make(map[string]json.RawMessage), nil
	}
	return root, nil
}

// writeCredentialsRoot writes the credentials root back to disk.
func writeCredentialsRoot(root map[string]json.RawMessage) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// SaveCredentials persists OAuth tokens to ~/.ycode/credentials.json under the "oauth" key.
func SaveCredentials(token *Token) error {
	root, err := readCredentialsRoot()
	if err != nil {
		return err
	}
	tokenData, err := json.Marshal(token)
	if err != nil {
		return err
	}
	root["oauth"] = json.RawMessage(tokenData)
	return writeCredentialsRoot(root)
}

// LoadCredentials reads OAuth tokens from ~/.ycode/credentials.json.
func LoadCredentials() (*Token, error) {
	root, err := readCredentialsRoot()
	if err != nil {
		return nil, err
	}
	raw, ok := root["oauth"]
	if !ok {
		return nil, fmt.Errorf("no oauth credentials found")
	}
	var token Token
	if err := json.Unmarshal(raw, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

// ClearCredentials removes the OAuth tokens from ~/.ycode/credentials.json.
func ClearCredentials() error {
	root, err := readCredentialsRoot()
	if err != nil {
		return err
	}
	delete(root, "oauth")
	return writeCredentialsRoot(root)
}

// OpenBrowser opens the given URL in the default browser.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/C", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// parseCallbackTarget parses the OAuth callback request target (path + query).
func parseCallbackTarget(target string) (*CallbackParams, error) {
	pathAndQuery := strings.SplitN(target, "?", 2)
	if pathAndQuery[0] != "/callback" {
		return nil, fmt.Errorf("unexpected callback path: %s", pathAndQuery[0])
	}
	if len(pathAndQuery) < 2 {
		return nil, fmt.Errorf("callback missing query parameters")
	}
	return parseCallbackQuery(pathAndQuery[1])
}

// parseCallbackQuery parses the query string from the OAuth callback.
func parseCallbackQuery(query string) (*CallbackParams, error) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return nil, fmt.Errorf("parse callback query: %w", err)
	}
	return &CallbackParams{
		Code:             values.Get("code"),
		State:            values.Get("state"),
		Error:            values.Get("error"),
		ErrorDescription: values.Get("error_description"),
	}, nil
}
