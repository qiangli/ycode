// Package memos provides a Go client for the Memos REST API.
// Used by ycode's agent tools to store and retrieve long-term memories.
package memos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to a Memos server's REST API.
type Client struct {
	baseURL    string // e.g. "http://127.0.0.1:12345"
	httpClient *http.Client
	token      string // access token or PAT
}

// NewClient creates a Memos API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetToken sets the bearer token for authenticated requests.
func (c *Client) SetToken(token string) {
	c.token = token
}

// Token returns the current auth token.
func (c *Client) Token() string {
	return c.token
}

// Memo represents a memo from the Memos API.
type Memo struct {
	Name        string       `json:"name"`    // "memos/{id}"
	State       string       `json:"state"`   // "NORMAL" | "ARCHIVED"
	Creator     string       `json:"creator"` // "users/{username}"
	CreateTime  string       `json:"createTime"`
	UpdateTime  string       `json:"updateTime"`
	DisplayTime string       `json:"displayTime"`
	Content     string       `json:"content"`
	Visibility  string       `json:"visibility"` // "PRIVATE" | "PROTECTED" | "PUBLIC"
	Tags        []string     `json:"tags"`
	Pinned      bool         `json:"pinned"`
	Property    MemoProperty `json:"property"`
	Snippet     string       `json:"snippet"`
}

// MemoProperty holds computed properties of a memo.
type MemoProperty struct {
	HasLink            bool   `json:"hasLink"`
	HasTaskList        bool   `json:"hasTaskList"`
	HasCode            bool   `json:"hasCode"`
	HasIncompleteTasks bool   `json:"hasIncompleteTasks"`
	Title              string `json:"title"`
}

// ID extracts the memo ID from the resource name (e.g. "memos/abc" -> "abc").
func (m *Memo) ID() string {
	if i := strings.LastIndex(m.Name, "/"); i >= 0 {
		return m.Name[i+1:]
	}
	return m.Name
}

// CreateMemoRequest is the request body for creating a memo.
type CreateMemoRequest struct {
	Memo   MemoInput `json:"memo"`
	MemoID string    `json:"memoId,omitempty"`
}

// MemoInput holds fields for creating/updating a memo.
type MemoInput struct {
	Content    string `json:"content"`
	Visibility string `json:"visibility,omitempty"` // defaults to "PRIVATE"
	Pinned     bool   `json:"pinned,omitempty"`
}

// UpdateMemoRequest is the request body for updating a memo.
type UpdateMemoRequest struct {
	Memo       UpdateMemoInput `json:"memo"`
	UpdateMask UpdateMask      `json:"updateMask"`
}

// UpdateMemoInput holds fields for updating a memo.
type UpdateMemoInput struct {
	Name       string `json:"name"`
	Content    string `json:"content,omitempty"`
	Visibility string `json:"visibility,omitempty"`
	Pinned     bool   `json:"pinned,omitempty"`
	State      string `json:"state,omitempty"`
}

// UpdateMask specifies which fields to update.
type UpdateMask struct {
	Paths []string `json:"paths"`
}

// ListMemosResponse is the response from listing memos.
type ListMemosResponse struct {
	Memos         []Memo `json:"memos"`
	NextPageToken string `json:"nextPageToken"`
}

// SignInRequest is used to authenticate.
type SignInRequest struct {
	PasswordCredentials *PasswordCredentials `json:"passwordCredentials,omitempty"`
}

// PasswordCredentials holds username/password for sign-in.
type PasswordCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthResponse contains tokens from sign-in.
type AuthResponse struct {
	Name        string `json:"name"`
	AccessToken string `json:"accessToken"`
}

// SignIn authenticates and stores the access token.
func (c *Client) SignIn(ctx context.Context, username, password string) error {
	req := SignInRequest{
		PasswordCredentials: &PasswordCredentials{
			Username: username,
			Password: password,
		},
	}
	var resp AuthResponse
	if err := c.post(ctx, "/api/v1/auth/signin", req, &resp); err != nil {
		return fmt.Errorf("sign in: %w", err)
	}
	c.token = resp.AccessToken
	return nil
}

// SignUp creates a new user account. Used for initial setup.
func (c *Client) SignUp(ctx context.Context, username, password string) error {
	req := map[string]any{
		"username": username,
		"password": password,
		"role":     "HOST",
	}
	var resp json.RawMessage
	if err := c.post(ctx, "/api/v1/users", req, &resp); err != nil {
		return fmt.Errorf("sign up: %w", err)
	}
	// Sign in to get the token.
	return c.SignIn(ctx, username, password)
}

// CreateMemo creates a new memo.
func (c *Client) CreateMemo(ctx context.Context, content, visibility string) (*Memo, error) {
	if visibility == "" {
		visibility = "PRIVATE"
	}
	req := CreateMemoRequest{
		Memo: MemoInput{
			Content:    content,
			Visibility: visibility,
		},
	}
	var memo Memo
	if err := c.post(ctx, "/api/v1/memos", req, &memo); err != nil {
		return nil, fmt.Errorf("create memo: %w", err)
	}
	return &memo, nil
}

// GetMemo retrieves a single memo by ID.
func (c *Client) GetMemo(ctx context.Context, memoID string) (*Memo, error) {
	var memo Memo
	if err := c.get(ctx, "/api/v1/memos/"+memoID, nil, &memo); err != nil {
		return nil, fmt.Errorf("get memo: %w", err)
	}
	return &memo, nil
}

// ListMemos lists memos with optional filtering.
func (c *Client) ListMemos(ctx context.Context, pageSize int, filter, pageToken string) (*ListMemosResponse, error) {
	params := url.Values{}
	if pageSize > 0 {
		params.Set("pageSize", fmt.Sprintf("%d", pageSize))
	}
	if filter != "" {
		params.Set("filter", filter)
	}
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}
	var resp ListMemosResponse
	if err := c.get(ctx, "/api/v1/memos", params, &resp); err != nil {
		return nil, fmt.Errorf("list memos: %w", err)
	}
	return &resp, nil
}

// SearchMemos searches memos by content substring.
func (c *Client) SearchMemos(ctx context.Context, query string, pageSize int) ([]Memo, error) {
	filter := fmt.Sprintf("content.contains(%q)", query)
	resp, err := c.ListMemos(ctx, pageSize, filter, "")
	if err != nil {
		return nil, err
	}
	return resp.Memos, nil
}

// SearchMemosByTag searches memos that have a specific tag.
func (c *Client) SearchMemosByTag(ctx context.Context, tag string, pageSize int) ([]Memo, error) {
	filter := fmt.Sprintf("%q in tags", tag)
	resp, err := c.ListMemos(ctx, pageSize, filter, "")
	if err != nil {
		return nil, err
	}
	return resp.Memos, nil
}

// UpdateMemo updates a memo's content.
func (c *Client) UpdateMemo(ctx context.Context, memoID, content string) (*Memo, error) {
	name := "memos/" + memoID
	req := UpdateMemoRequest{
		Memo: UpdateMemoInput{
			Name:    name,
			Content: content,
		},
		UpdateMask: UpdateMask{Paths: []string{"content"}},
	}
	var memo Memo
	if err := c.patch(ctx, "/api/v1/memos/"+memoID, req, &memo); err != nil {
		return nil, fmt.Errorf("update memo: %w", err)
	}
	return &memo, nil
}

// DeleteMemo deletes a memo by ID.
func (c *Client) DeleteMemo(ctx context.Context, memoID string) error {
	if err := c.del(ctx, "/api/v1/memos/"+memoID); err != nil {
		return fmt.Errorf("delete memo: %w", err)
	}
	return nil
}

// Healthy checks if the server is reachable.
func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// --- HTTP helpers ---

func (c *Client) post(ctx context.Context, path string, body, result any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, result)
}

func (c *Client) patch(ctx context.Context, path string, body, result any) error {
	return c.doJSON(ctx, http.MethodPatch, path, body, result)
}

func (c *Client) del(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.readError(resp)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, params url.Values, result any) error {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.readError(resp)
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.readError(resp)
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func (c *Client) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func (c *Client) readError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("memos API error (HTTP %d): %s", resp.StatusCode, string(body))
}
