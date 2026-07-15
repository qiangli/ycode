package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Inspect or set model configuration",
	}
	cmd.AddCommand(
		newModelCurrentCmd(),
		newModelUseCmd(),
		newModelP2PCmd(),
	)
	return cmd
}

// newModelP2PCmd resolves the model your p2p mesh recommends for a capability
// (chat/vision) via cloudbox's serving-plane resolver and sets it as the
// default. When you're on the same LAN as a serving host it also reports the
// direct LAN endpoint (lower latency, bypasses the cloud relay).
func newModelP2PCmd() *cobra.Command {
	var capability string
	cmd := &cobra.Command{
		Use:   "p2p",
		Short: "Set the model your p2p mesh recommends (via cloudbox's serving-plane resolver)",
		Long: `Queries cloudbox's p2p serving-plane resolver (GET /api/v1/p2p/model?cap=)
for the recommended warm model of a capability, sets it as the default in
settings.json, and reports the endpoint to use — the direct LAN URL when you are
co-located with a serving host, else the cloud gateway.

Uses DHNT_BASE_URL + DHNT_API_KEY (the cloudbox gateway + token; source
~/.config/ycode/cloudbox-env.sh).

Examples:
  ycode model p2p                 # best warm chat model
  ycode model p2p --cap vision    # best warm vision model`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return resolveP2PModel(cmd.Context(), capability)
		},
	}
	cmd.Flags().StringVar(&capability, "cap", "chat", "capability: chat | vision")
	return cmd
}

func resolveP2PModel(ctx context.Context, capability string) error {
	base := strings.TrimSpace(os.Getenv("DHNT_BASE_URL"))
	if base == "" {
		return fmt.Errorf("DHNT_BASE_URL not set — source ~/.config/ycode/cloudbox-env.sh")
	}
	key := strings.TrimSpace(os.Getenv("DHNT_API_KEY"))
	// DHNT_BASE_URL is the OpenAI base (e.g. https://host/v1); the resolver lives
	// at <origin>/api/v1/p2p/model. Strip a trailing /v1 to get the origin.
	origin := strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1")
	u := origin + "/api/v1/p2p/model?cap=" + url.QueryEscape(capability)

	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("p2p resolver %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out struct {
		Model    string `json:"model"`
		Endpoint string `json:"endpoint"`
		Scope    string `json:"scope"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("decode resolver response: %w", err)
	}
	if out.Model == "" {
		return fmt.Errorf("resolver returned no model for cap=%s", capability)
	}

	path, err := userConfigPath()
	if err != nil {
		return err
	}
	m, err := loadConfig(path)
	if err != nil {
		return err
	}
	m["model"] = out.Model
	if err := saveConfig(path, m); err != nil {
		return err
	}
	fmt.Printf("default model set to %q (p2p %s) in %s\n", out.Model, capability, path)
	if out.Endpoint != "" {
		fmt.Printf("endpoint: %s (%s)\n", out.Endpoint, out.Scope)
		if out.Scope == "lan" {
			fmt.Printf("  same-LAN direct path — for lowest latency:  export DHNT_BASE_URL=%s\n", out.Endpoint)
		}
	}
	return nil
}

// newModelCurrentCmd prints the configured default model from
// ~/.config/ycode/settings.json. Convenience for `ycode config get model`.
func newModelCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Print the configured default model (settings.json `model` field)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := userConfigPath()
			if err != nil {
				return err
			}
			m, err := loadConfig(path)
			if err != nil {
				return err
			}
			if v, ok := m["model"].(string); ok && v != "" {
				fmt.Println(v)
				return nil
			}
			fmt.Fprintln(os.Stderr, "no default model set; use `ycode model use <name>`")
			return nil
		},
	}
}

// newModelUseCmd sets ~/.config/ycode/settings.json `model` to <name>.
// Equivalent to `ycode config set model <name>`.
func newModelUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <model>",
		Short: "Set the default model in settings.json",
		Long: `Sets the ` + "`model`" + ` field in ~/.config/ycode/settings.json.
Provider selection remains the normal runtime provider resolution path.

Examples:
  ycode model use claude-sonnet-4-6
  ycode model use gpt-4o-mini
  ycode model use kimi-k2.5
  ycode model use deepseek-chat`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := userConfigPath()
			if err != nil {
				return err
			}
			m, err := loadConfig(path)
			if err != nil {
				return err
			}
			m["model"] = args[0]
			if err := saveConfig(path, m); err != nil {
				return err
			}
			fmt.Printf("default model set to %q in %s\n", args[0], path)
			return nil
		},
	}
}
