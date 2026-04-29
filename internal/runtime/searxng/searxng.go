// Package searxng provides a containerized SearXNG meta-search service.
//
// SearXNG (AGPL-3.0) runs as an isolated container service, queried via HTTP
// JSON API. ycode never links, embeds, or modifies SearXNG code -- it is a
// client making HTTP requests to an independent service. The AGPL does not
// propagate through HTTP API boundaries.
package searxng

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/container"
)

const (
	// containerImage is the official SearXNG container image.
	containerImage = "docker.io/searxng/searxng:latest"

	// containerPort is the port SearXNG listens on inside the container.
	containerPort = 8080

	// healthTimeout is how long to wait for SearXNG to become healthy.
	healthTimeout = 60 * time.Second

	// searchTimeout is the timeout for individual search requests.
	searchTimeout = 15 * time.Second
)

// Service manages a containerized SearXNG instance.
type Service struct {
	engine    *container.Engine
	sessionID string
	network   string

	mu       sync.Mutex
	ctr      *container.Container
	hostPort uint16
	started  bool
}

// NewService creates a new SearXNG service manager.
func NewService(engine *container.Engine, sessionID string, network string) *Service {
	return &Service{
		engine:    engine,
		sessionID: sessionID,
		network:   network,
	}
}

// Start pulls the SearXNG image and starts the container.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	slog.Info("searxng: pulling image", "image", containerImage)
	if err := s.engine.PullImage(ctx, containerImage); err != nil {
		return fmt.Errorf("searxng: pull image: %w", err)
	}

	// Find a free port on the host.
	port, err := freePort()
	if err != nil {
		return fmt.Errorf("searxng: find free port: %w", err)
	}

	name := fmt.Sprintf("ycode-searxng-%s", s.sessionID)
	cfg := &container.ContainerConfig{
		Name:  name,
		Image: containerImage,
		Ports: []container.PortMapping{
			{HostPort: port, ContainerPort: containerPort, Protocol: "tcp"},
		},
		Network: s.network,
		Labels: map[string]string{
			"ycode.session":   s.sessionID,
			"ycode.component": "searxng",
		},
		Env: map[string]string{
			"SEARXNG_BASE_URL": fmt.Sprintf("http://localhost:%d/", containerPort),
		},
		Init:    true,
		CapDrop: []string{"ALL"},
		Tmpfs:   []string{"/tmp"},
	}

	ctr, err := s.engine.CreateContainer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("searxng: create container: %w", err)
	}

	if err := ctr.Start(ctx); err != nil {
		ctr.Remove(ctx, true)
		return fmt.Errorf("searxng: start container: %w", err)
	}

	s.ctr = ctr
	s.hostPort = port
	s.started = true

	slog.Info("searxng: container started", "name", name, "port", port)

	// Wait for health check in background -- don't block Start.
	go func() {
		if err := s.waitHealthy(context.Background()); err != nil {
			slog.Warn("searxng: health check failed, service may not be ready", "error", err)
		}
	}()

	return nil
}

// Stop stops and removes the SearXNG container.
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started || s.ctr == nil {
		return nil
	}

	slog.Info("searxng: stopping container")
	s.ctr.Stop(ctx, 10*time.Second)
	s.ctr.Remove(ctx, true)
	s.started = false
	s.ctr = nil
	return nil
}

// Available returns true if the SearXNG service is running.
func (s *Service) Available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// BaseURL returns the HTTP URL for the running SearXNG instance.
func (s *Service) BaseURL() string {
	return fmt.Sprintf("http://localhost:%d", s.hostPort)
}

// SearchResult represents a single result from SearXNG.
type SearchResult struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Content       string `json:"content"`
	PublishedDate string `json:"publishedDate,omitempty"`
	Engine        string `json:"engine,omitempty"`
}

// Search queries SearXNG and returns structured results.
func (s *Service) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if !s.Available() {
		return nil, fmt.Errorf("searxng: service not running")
	}

	if maxResults <= 0 {
		maxResults = 10
	}

	client := &http.Client{Timeout: searchTimeout}
	u := fmt.Sprintf("%s/search?q=%s&format=json&pageno=1",
		s.BaseURL(), url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng: query failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("searxng: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var searchResp struct {
		Results []SearchResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("searxng: decode response: %w", err)
	}

	results := searchResp.Results
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}

// waitHealthy polls the SearXNG healthcheck endpoint until ready or timeout.
func (s *Service) waitHealthy(ctx context.Context) error {
	deadline := time.Now().Add(healthTimeout)
	client := &http.Client{Timeout: 2 * time.Second}
	u := fmt.Sprintf("%s/healthz", s.BaseURL())

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(u)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				slog.Info("searxng: healthy")
				return nil
			}
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("searxng: health check timed out after %v", healthTimeout)
}

// freePort finds an available TCP port on the host.
func freePort() (uint16, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return uint16(l.Addr().(*net.TCPAddr).Port), nil
}
