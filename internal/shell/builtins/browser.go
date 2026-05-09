package builtins

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func init() { Register(&browserVerb{}) }

type browserVerb struct{}

func (browserVerb) Name() string        { return "browser" }
func (browserVerb) Description() string { return "Browser automation: open, fetch, find (lightweight HTTP fallback when no headless browser)" }
func (browserVerb) Usage() string       { return "yc browser fetch <url> | open <url> | find <url> <selector>" }

func (browserVerb) Run(ctx context.Context, args []string, stdio Stdio, _ string) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc browser: missing subcommand (fetch | open | find)")
		return 2, nil
	}
	switch args[0] {
	case "fetch":
		return browserFetch(ctx, args[1:], stdio)
	case "open":
		fmt.Fprintln(stdio.Stderr, "yc browser open: requires the headless browser running inside `ycode serve`")
		fmt.Fprintln(stdio.Stderr, "  for static HTML, try: yc browser fetch <url>")
		return 1, nil
	case "find":
		fmt.Fprintln(stdio.Stderr, "yc browser find: requires the headless browser running inside `ycode serve`")
		return 1, nil
	default:
		fmt.Fprintf(stdio.Stderr, "yc browser: unknown subcommand %q\n", args[0])
		return 2, nil
	}
}

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
