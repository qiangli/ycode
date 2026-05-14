package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/mcpservers"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/probe"
)

func init() { Register(&browserVerb{}) }

type browserVerb struct{}

func (browserVerb) Name() string { return "browser" }
func (browserVerb) Description() string {
	return "Browser automation: open, fetch, find. Uses configured probe mode if reachable; falls back to HTTP for fetch."
}
func (browserVerb) Usage() string {
	return "yc browser fetch <url> | open <url> | find <url> <selector>"
}

func (browserVerb) Run(ctx context.Context, args []string, stdio Stdio, _ string) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc browser: missing subcommand (fetch | open | find)")
		return 2, nil
	}
	switch args[0] {
	case "fetch":
		return browserFetch(ctx, args[1:], stdio)
	case "open":
		return browserOpen(ctx, args[1:], stdio)
	case "find":
		return browserFind(ctx, args[1:], stdio)
	default:
		fmt.Fprintf(stdio.Stderr, "yc browser: unknown subcommand %q\n", args[0])
		return 2, nil
	}
}

// browserFetch is the always-on HTTP fallback. No JS execution, no
// DOM — just an unauthenticated GET with a small body cap. Useful
// when no Chrome is around and the page is static HTML.
func browserFetch(ctx context.Context, args []string, stdio Stdio) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc browser fetch: missing URL")
		return 2, nil
	}
	url := args[0]
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		fmt.Fprintln(stdio.Stderr, "yc browser fetch: URL must start with http:// or https://")
		return 2, nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc browser fetch: %v\n", err)
		return 1, nil
	}
	req.Header.Set("User-Agent", "ycode-shell/0.1 (+https://github.com/qiangli/ycode)")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc browser fetch: %v\n", err)
		return 1, nil
	}
	defer resp.Body.Close()

	fmt.Fprintf(stdio.Stderr, "# %s %s — %d %s (Content-Type: %s)\n",
		req.Method, url, resp.StatusCode, resp.Status, resp.Header.Get("Content-Type"))

	const maxBytes = 1 << 20 // 1 MB
	body := make([]byte, maxBytes)
	n, _ := resp.Body.Read(body)
	stdio.Stdout.Write(body[:n])
	if resp.StatusCode >= 400 {
		return 1, nil
	}
	return 0, nil
}

// browserOpen attaches to probe Chrome if configured + reachable, then
// navigates and prints the extracted content. Falls back to fetch
// when probe isn't usable so the verb is always productive.
func browserOpen(ctx context.Context, args []string, stdio Stdio) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc browser open: missing URL")
		return 2, nil
	}
	url := args[0]
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		fmt.Fprintln(stdio.Stderr, "yc browser open: URL must start with http:// or https://")
		return 2, nil
	}

	svc, mode, ok := attachProbe(ctx, stdio)
	if !ok {
		fmt.Fprintf(stdio.Stderr, "# yc browser open: probe mode not available (configured=%q); falling back to HTTP fetch\n", mode)
		return browserFetch(ctx, args, stdio)
	}
	defer svc.Stop(ctx)

	res, err := svc.Execute(ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionNavigate,
		URL:  url,
	})
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc browser open: %v\n", err)
		return 1, nil
	}
	return printBrowserResult(stdio, res)
}

// browserFind navigates to url, queries the selector, and prints the
// matched element's outerHTML + textContent. Requires probe — there
// is no HTML-fetch fallback that can execute a CSS selector
// faithfully against the rendered DOM.
func browserFind(ctx context.Context, args []string, stdio Stdio) (int, error) {
	if len(args) < 2 {
		fmt.Fprintln(stdio.Stderr, "yc browser find: usage: yc browser find <url> <selector>")
		return 2, nil
	}
	url, selector := args[0], args[1]
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		fmt.Fprintln(stdio.Stderr, "yc browser find: URL must start with http:// or https://")
		return 2, nil
	}

	svc, mode, ok := attachProbe(ctx, stdio)
	if !ok {
		fmt.Fprintf(stdio.Stderr, "yc browser find: probe mode required (configured=%q). Start probe with `ycode browser launch` and set `browser.mode=probe`.\n", mode)
		return 1, nil
	}
	defer svc.Stop(ctx)

	if _, err := svc.Execute(ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionNavigate,
		URL:  url,
	}); err != nil {
		fmt.Fprintf(stdio.Stderr, "yc browser find: navigate: %v\n", err)
		return 1, nil
	}
	// Use Evaluate to read the matched element — chromedp's
	// chromedp.OuterHTML expects a selector and writes to a *string,
	// but Evaluate keeps the snippet self-contained and JSON-safe
	// regardless of which selectors match.
	script := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) return JSON.stringify({matched: false});
		return JSON.stringify({
			matched: true,
			outerHTML: el.outerHTML,
			textContent: el.textContent,
			tagName: el.tagName
		});
	})()`, selector)
	res, err := svc.Execute(ctx, mcpservers.BrowserAction{
		Type:   mcpservers.ActionEvaluate,
		Script: script,
	})
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc browser find: evaluate: %v\n", err)
		return 1, nil
	}
	if !res.Success {
		fmt.Fprintf(stdio.Stderr, "yc browser find: %s\n", res.Error)
		return 1, nil
	}
	// Evaluate returns the JS expression's value in res.Data as a
	// Go-printed value. Unwrap the JSON string so the user sees clean
	// JSON, not "<map[...]>".
	if strings.HasPrefix(res.Data, `"`) && strings.HasSuffix(res.Data, `"`) {
		var unq string
		if err := json.Unmarshal([]byte(res.Data), &unq); err == nil {
			res.Data = unq
		}
	}
	fmt.Fprintln(stdio.Stdout, res.Data)
	return 0, nil
}

// attachProbe loads ycode config, checks whether probe mode is
// configured and reachable, and returns a freshly-attached probe
// Service the caller is responsible for stopping. The mode string is
// returned regardless of success so callers can include it in
// diagnostic messages.
func attachProbe(ctx context.Context, stdio Stdio) (*probe.Service, string, bool) {
	cfg := loadConfigBestEffort()
	mode := ""
	if cfg != nil && cfg.Browser != nil {
		mode = cfg.Browser.Mode
	}
	if mode != mcpservers.ModeProbe {
		return nil, mode, false
	}
	url := ""
	if cfg.Browser.ProbeURL != "" {
		url = cfg.Browser.ProbeURL
	}
	svc := probe.New(url)
	if !svc.Available(ctx) {
		fmt.Fprintf(stdio.Stderr, "# yc browser: probe target %s not reachable — start Chrome with `ycode browser launch`\n", svc.URL())
		return nil, mode, false
	}
	if err := svc.EnsureReady(ctx); err != nil {
		fmt.Fprintf(stdio.Stderr, "# yc browser: attach failed: %v\n", err)
		return nil, mode, false
	}
	return svc, mode, true
}

// loadConfigBestEffort reads the three-tier config silently. Used
// only to discover browser.mode + probeURL; failure is non-fatal
// (callers fall back to no-browser behavior).
func loadConfigBestEffort() *config.Config {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	userDir := filepath.Join(home, ".config", "ycode")
	projectDir := filepath.Join(cwd, ".agents", "ycode")
	loader := config.NewLoader(userDir, projectDir, projectDir)
	cfg, err := loader.Load()
	if err != nil {
		return nil
	}
	return cfg
}

func printBrowserResult(stdio Stdio, res *mcpservers.BrowserResult) (int, error) {
	if res == nil {
		fmt.Fprintln(stdio.Stderr, "yc browser: no result")
		return 1, nil
	}
	if !res.Success {
		if res.Error != "" {
			fmt.Fprintln(stdio.Stderr, "yc browser:", res.Error)
		}
		return 1, nil
	}
	if res.Title != "" {
		fmt.Fprintf(stdio.Stderr, "# %s — %s\n", res.Title, res.URL)
	} else if res.URL != "" {
		fmt.Fprintf(stdio.Stderr, "# %s\n", res.URL)
	}
	if res.Content != "" {
		fmt.Fprintln(stdio.Stdout, res.Content)
	} else if res.Data != "" {
		fmt.Fprintln(stdio.Stdout, res.Data)
	}
	return 0, nil
}
