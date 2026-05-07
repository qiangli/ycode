package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/runtime/computer"
	netutil "github.com/qiangli/ycode/internal/runtime/net"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// webGateway is the package-level Web surface used by handlers.
// When RegisterWebHandlers has not run yet (e.g. unit tests calling
// handleWebFetch directly), getWebGateway returns a process-default
// Web that wraps a fresh *http.Client with SSRF protection.
var (
	webGatewayMu sync.RWMutex
	webGateway   computer.Web
)

func getWebGateway() computer.Web {
	webGatewayMu.RLock()
	w := webGateway
	webGatewayMu.RUnlock()
	if w != nil {
		return w
	}
	return defaultWeb{}
}

func setWebGateway(w computer.Web) {
	webGatewayMu.Lock()
	webGateway = w
	webGatewayMu.Unlock()
}

// defaultWeb is the fallback Web used when no gateway has been
// registered. Mirrors LocalComputer's Web semantics but does not
// require a VFS.
type defaultWeb struct{}

func (defaultWeb) Fetch(ctx context.Context, rawURL string, opts computer.FetchOpts) (*computer.FetchResult, error) {
	if err := netutil.ValidateURL(rawURL); err != nil {
		return nil, fmt.Errorf("SSRF protection: %w", err)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if err := netutil.ValidateURL(req.URL.String()); err != nil {
				return fmt.Errorf("SSRF protection on redirect: %w", err)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = "ycode/1.0"
	}
	req.Header.Set("User-Agent", ua)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	max := opts.MaxBytes
	if max <= 0 {
		max = 1 << 20
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	final := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	return &computer.FetchResult{
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   body,
		URL:    final,
	}, nil
}

func (defaultWeb) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req.URL != nil {
		if err := netutil.ValidateURL(req.URL.String()); err != nil {
			return nil, fmt.Errorf("SSRF protection: %w", err)
		}
	}
	if req.Context() == nil || req.Context() == context.Background() {
		req = req.WithContext(ctx)
	}
	return http.DefaultClient.Do(req)
}

// RegisterWebHandlers registers WebFetch and WebSearch handlers.
// The gateway is captured here so the inner closures can stay
// signature-compatible with the registry's Handler type.
func RegisterWebHandlers(r *Registry, web computer.Web) {
	setWebGateway(web)
	if spec, ok := r.Get("WebFetch"); ok {
		spec.Handler = handleWebFetch
	}
	if spec, ok := r.Get("WebSearch"); ok {
		spec.Handler = handleWebSearch
	}
}

func handleWebFetch(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		URL          string `json:"url"`
		Prompt       string `json:"prompt,omitempty"`
		OutputFormat string `json:"output_format,omitempty"`
		MaxLength    int    `json:"max_length,omitempty"`
		ClickLink    int    `json:"click_link,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse WebFetch input: %w", err)
	}

	// Text browser fallback: click_link resolves a link from the previous fetch.
	if params.ClickLink > 0 {
		link, ok := lookupLink(params.ClickLink)
		if !ok {
			return "", fmt.Errorf("link [%d] not found (use WebFetch with a URL first)", params.ClickLink)
		}
		params.URL = link.Href
	}

	if params.URL == "" {
		return "", fmt.Errorf("url is required (or use click_link to follow a link from the previous page)")
	}

	host := ""
	if u, perr := url.Parse(params.URL); perr == nil {
		host = u.Host
	}

	started := time.Now()
	res, err := getWebGateway().Fetch(ctx, params.URL, computer.FetchOpts{
		UserAgent: "ycode/1.0",
		Timeout:   30 * time.Second,
	})
	if err != nil {
		yotel.RecordWebFetch(ctx, host, 0, time.Since(started), 0, false)
		return "", err
	}
	yotel.RecordWebFetch(ctx, host, res.Status, time.Since(started), len(res.Body), res.Status/100 == 2)

	text, err := extractContent(string(res.Body), params.URL, params.OutputFormat, params.MaxLength)
	if err != nil {
		return "", fmt.Errorf("extract content: %w", err)
	}

	return text, nil
}

func handleWebSearch(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse WebSearch input: %w", err)
	}

	started := time.Now()
	resp, err := searchWithFallback(ctx, params.Query, params.MaxResults)
	if err != nil {
		yotel.RecordWebSearch(ctx, "fallback", time.Since(started), 0, false)
		return "", fmt.Errorf("search: %w", err)
	}
	yotel.RecordWebSearch(ctx, "fallback", time.Since(started), len(resp.Results), true)

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal results: %w", err)
	}
	return string(out), nil
}

// stripHTML does a basic HTML to text conversion.
func stripHTML(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Collapse whitespace.
	result := b.String()
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}
