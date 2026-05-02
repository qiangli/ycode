package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with a remote A2A agent.
type Client struct {
	endpoint   string
	httpClient *http.Client
	auth       *AuthConfig
}

// NewClient creates an A2A client for the given endpoint.
func NewClient(endpoint string, auth *AuthConfig) *Client {
	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		auth: auth,
	}
}

// FetchCard retrieves the agent card from the well-known endpoint.
func (c *Client) FetchCard(ctx context.Context) (*AgentCard, error) {
	url := c.endpoint + "/.well-known/agent-card.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch agent card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent card returned status %d", resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("decode agent card: %w", err)
	}
	return &card, nil
}

// SendTask sends a task to the remote agent and waits for completion.
func (c *Client) SendTask(ctx context.Context, task *TaskRequest) (*TaskResponse, error) {
	url := c.endpoint + "/a2a/tasks/send"
	body, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("marshal task: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("task send returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var taskResp TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, fmt.Errorf("decode task response: %w", err)
	}
	return &taskResp, nil
}

// applyAuth adds authentication headers to the request.
func (c *Client) applyAuth(req *http.Request) {
	if c.auth == nil || c.auth.Token == "" {
		return
	}

	switch c.auth.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.auth.Token)
	case "api_key":
		header := c.auth.Header
		if header == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, c.auth.Token)
	default:
		req.Header.Set("Authorization", "Bearer "+c.auth.Token)
	}
}
