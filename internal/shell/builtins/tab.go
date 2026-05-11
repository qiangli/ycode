package builtins

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func init() { Register(&tabVerb{}) }

// tabVerb is the bash-callable surface for the live-mode browser hub.
// All subcommands hit the local hub's `/dispatch` endpoint at
// 127.0.0.1:58082 (override with YCODE_LIVE_PORT). They print JSON-
// friendly output suitable for piping to `jq` or for an agent to
// parse.
type tabVerb struct{}

func (tabVerb) Name() string { return "tab" }
func (tabVerb) Description() string {
	return "Drive the connected Chrome tab (live mode): extract / screenshot / navigate / click / type / scroll / back / tabs / status"
}
func (tabVerb) Usage() string {
	return "yc tab status | extract | screenshot [path] | navigate <url> | click <selector> | type <selector> <text> | scroll [up|down] [px] | back | tabs [list|switch|new|close] [index]"
}

func (tabVerb) Run(ctx context.Context, args []string, stdio Stdio, _ string) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc tab: missing subcommand. Try `yc tab status`.")
		return 2, nil
	}
	switch args[0] {
	case "status":
		return tabStatus(ctx, stdio)
	case "extract":
		return tabDispatch(ctx, stdio, "extract", nil)
	case "screenshot":
		return tabScreenshot(ctx, args[1:], stdio)
	case "navigate":
		if len(args) < 2 {
			fmt.Fprintln(stdio.Stderr, "yc tab navigate: missing URL")
			return 2, nil
		}
		return tabDispatch(ctx, stdio, "navigate", map[string]any{"url": args[1]})
	case "click":
		if len(args) < 2 {
			fmt.Fprintln(stdio.Stderr, "yc tab click: missing selector")
			return 2, nil
		}
		return tabDispatch(ctx, stdio, "click", map[string]any{"selector": args[1]})
	case "type":
		if len(args) < 3 {
			fmt.Fprintln(stdio.Stderr, "yc tab type: usage `yc tab type <selector> <text>`")
			return 2, nil
		}
		return tabDispatch(ctx, stdio, "type", map[string]any{
			"selector": args[1],
			"text":     strings.Join(args[2:], " "),
		})
	case "scroll":
		direction := "down"
		amount := 500
		if len(args) >= 2 {
			direction = args[1]
		}
		if len(args) >= 3 {
			if v, err := strconv.Atoi(args[2]); err == nil {
				amount = v
			}
		}
		return tabDispatch(ctx, stdio, "scroll", map[string]any{"direction": direction, "amount": amount})
	case "back":
		return tabDispatch(ctx, stdio, "back", nil)
	case "tabs":
		action := "list"
		if len(args) >= 2 {
			action = args[1]
		}
		params := map[string]any{"action": action}
		if len(args) >= 3 {
			if v, err := strconv.Atoi(args[2]); err == nil {
				params["tab_id"] = v
			}
		}
		return tabDispatch(ctx, stdio, "tabs", params)
	}
	fmt.Fprintf(stdio.Stderr, "yc tab: unknown subcommand %q\n", args[0])
	return 2, nil
}

// hubURL returns the base URL of the live hub. Honors YCODE_LIVE_PORT
// for sessions that picked a non-default port.
func hubURL() string {
	port := os.Getenv("YCODE_LIVE_PORT")
	if port == "" {
		port = "58082"
	}
	return "http://127.0.0.1:" + port
}

// tabStatus is the fastest pre-flight: confirms the hub is running
// AND the extension's WS is up. Exit 0 = ready, 1 = not.
func tabStatus(ctx context.Context, stdio Stdio) (int, error) {
	base := hubURL()
	c := &http.Client{Timeout: 700 * time.Millisecond}

	// /health
	resp, err := c.Get(base + "/health")
	if err != nil {
		fmt.Fprintf(stdio.Stdout, "hub: down (%v)\n", err)
		fmt.Fprintln(stdio.Stdout, "fix: start `bin/ycode serve` (or any `bin/ycode prompt`) with browser.mode=live")
		return 1, nil
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stdio.Stdout, "hub: unhealthy (%d)\n", resp.StatusCode)
		return 1, nil
	}

	// /connected
	resp, err = c.Get(base + "/connected")
	if err != nil {
		fmt.Fprintln(stdio.Stdout, "hub: up but /connected unreachable")
		return 1, nil
	}
	defer resp.Body.Close()
	var s struct {
		Connected bool `json:"connected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		fmt.Fprintf(stdio.Stdout, "hub: up, status payload unreadable (%v)\n", err)
		return 1, nil
	}
	if !s.Connected {
		fmt.Fprintln(stdio.Stdout, "hub: up   extension: DISCONNECTED")
		fmt.Fprintln(stdio.Stdout, "fix: click the ycode-live extension icon on the target tab → Connect")
		return 1, nil
	}
	fmt.Fprintln(stdio.Stdout, "hub: up   extension: connected")
	return 0, nil
}

// tabDispatch POSTs {method, params} to /dispatch and pretty-prints
// the JSON result (or error) to stdout.
func tabDispatch(ctx context.Context, stdio Stdio, method string, params map[string]any) (int, error) {
	if params == nil {
		params = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{"method": method, "params": params})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, hubURL()+"/dispatch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 35 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc tab %s: %v\n", method, err)
		fmt.Fprintln(stdio.Stderr, "  is the hub running? try `yc tab status`")
		return 1, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusServiceUnavailable {
		fmt.Fprintln(stdio.Stderr, "yc tab: extension not connected — open the popup and click Connect on the target tab")
		return 1, nil
	}
	if resp.StatusCode >= 400 {
		fmt.Fprintf(stdio.Stderr, "yc tab %s: hub returned %d: %s\n", method, resp.StatusCode, string(raw))
		return 1, nil
	}

	// Re-encode for nice formatting.
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		stdio.Stdout.Write(raw)
		return 0, nil
	}
	enc := json.NewEncoder(stdio.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return 0, nil
}

// tabScreenshot wraps `extract` for screenshots: pull the base64
// payload out of the JSON and write the PNG bytes to a path so the
// caller can preview the file directly (or to stdout if no path).
func tabScreenshot(ctx context.Context, args []string, stdio Stdio) (int, error) {
	body, _ := json.Marshal(map[string]any{"method": "screenshot", "params": map[string]any{}})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, hubURL()+"/dispatch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc tab screenshot: %v\n", err)
		return 1, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		fmt.Fprintln(stdio.Stderr, "yc tab screenshot: extension not connected")
		return 1, nil
	}

	var out struct {
		Result struct {
			Image string `json:"image"`
		} `json:"result"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		fmt.Fprintf(stdio.Stderr, "yc tab screenshot: bad response: %v\n", err)
		return 1, nil
	}
	if out.Error != "" {
		fmt.Fprintf(stdio.Stderr, "yc tab screenshot: %s\n", out.Error)
		return 1, nil
	}
	if out.Result.Image == "" {
		fmt.Fprintln(stdio.Stderr, "yc tab screenshot: empty image payload")
		return 1, nil
	}
	raw, err := base64.StdEncoding.DecodeString(out.Result.Image)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc tab screenshot: decode: %v\n", err)
		return 1, nil
	}

	dest := ""
	if len(args) > 0 {
		dest = args[0]
	}
	if dest == "" {
		// Default: write to /tmp with a timestamp so repeated calls
		// produce distinct files the agent can reference.
		dest = fmt.Sprintf("/tmp/yc-tab-%d.png", time.Now().Unix())
	}
	if err := os.WriteFile(dest, raw, 0o644); err != nil {
		fmt.Fprintf(stdio.Stderr, "yc tab screenshot: write %s: %v\n", dest, err)
		return 1, nil
	}
	fmt.Fprintln(stdio.Stdout, dest)
	return 0, nil
}
