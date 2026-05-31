package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/inference"
	"github.com/qiangli/ycode/pkg/ollm"
)

// newOllamaCmd is a drop-in shim for the upstream `ollama` CLI, backed
// by ycode's embedded ollama HTTP server (running on whatever
// OLLAMA_HOST resolves to — default 127.0.0.1:11434). Verbs map to
// either the local /api/* surface (pull/list/rm/ps/show), `ycode
// serve` (serve), or `ycode --model …` (run).
//
// Symlinking ycode onto PATH as `ollama` does NOT automatically route
// to this subcommand — cobra dispatches on argv[1], not argv[0]. The
// expected drop-in path is a tiny wrapper or alias:
//
//	alias ollama='ycode ollama'
//
// or a shell script that does `exec ycode ollama "$@"`.
func newOllamaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Ollama-compatible CLI (shim onto ycode's embedded ollama server)",
		Long: `Drop-in shim for the upstream ollama CLI. Each verb maps to either
the embedded server's /api/* HTTP surface or to ycode itself:

  ollama serve            → bind embedded ollama on $OLLAMA_HOST (foreground)
  ollama pull MODEL       → POST /api/pull
  ollama list / ls        → GET  /api/tags
  ollama rm MODEL         → DELETE /api/delete
  ollama ps               → GET  /api/ps
  ollama show MODEL       → POST /api/show
  ollama run MODEL [...]  → ycode --model MODEL (interactive REPL,
                            or one-shot via ycode prompt if args given)
  ollama --version        → ycode version

All HTTP calls go to whatever OLLAMA_HOST resolves to. ` + "`ollama serve`" + ` runs
the embedded ollama HTTP server in the foreground (just the ollama
component — no observability stack); for the full ycode stack run
` + "`ycode serve`" + ` separately.`,
	}

	cmd.AddCommand(
		newOllamaServeCmd(),
		newOllamaPullCmd(),
		newOllamaListCmd(),
		newOllamaRmCmd(),
		newOllamaPsCmd(),
		newOllamaShowCmd(),
		newOllamaRunCmd(),
		newOllamaVersionCmd(),
	)
	return cmd
}

// ollamaBaseURL resolves the URL the shim talks to. Identical policy
// to the in-process server's bind: OLLAMA_HOST > built-in default.
func ollamaBaseURL() string {
	return inference.DefaultOllamaURL()
}

// newOllamaServeCmd matches the upstream `ollama serve` UX: bind the
// embedded ollama HTTP server on $OLLAMA_HOST (default 127.0.0.1:11434)
// in the foreground, log readiness, and stop on SIGINT/SIGTERM.
//
// Previously this exec'd into `ycode serve`, which brings up the full
// observability stack (vlogs, jaeger, perses, gitea, NATS, OTLP) and
// defaults to detaching to a background process. Two failure modes
// surfaced:
//
//  1. The detacher printed "Server started in background (PID N)" even
//     when the child immediately exited because another `ycode serve`
//     was already running — misleading the user.
//  2. Even when serve came up cleanly, the ollama component is silently
//     skipped when the binary was built without `-tags embed_runner`,
//     so 11434 wasn't bound and subsequent `ollama list` calls hit
//     `connection refused`.
//
// The standalone-component path is what an `ollama serve` user
// expects: synchronous bind, immediate "listening on …" log, Ctrl-C
// to stop. `ErrRunnerNotInstalled` and port-collision errors classify
// cleanly under the selfheal wrapper (FailureTypeNotInstalled and
// FailureTypePortInUse) so the user gets actionable output instead of
// a silent "started" lie.
func newOllamaServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Bind the embedded ollama HTTP server on $OLLAMA_HOST (foreground)",
		Long: `Run the embedded ollama server in the foreground on whatever
OLLAMA_HOST resolves to (default 127.0.0.1:11434). Ctrl-C to stop.

This is the standalone ollama component — no observability stack,
no API/NATS/Gitea. For the full ycode stack run ` + "`ycode serve`" + ` separately
(it also embeds ollama when built with -tags embed_runner).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOllamaServe(cmd.Context())
		},
	}
}

// runOllamaServe boots just the OllamaComponent and blocks until a
// terminating signal arrives. Shares dataDir with `ycode serve` so
// pulled models are visible to both.
func runOllamaServe(ctx context.Context) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("user home dir: %w", err)
	}

	// In system-binaries mode (--use-system-binaries or
	// inference.useSystem: true) this command is a no-op: the user runs
	// upstream `ollama serve` themselves. Refuse cleanly with a hint
	// rather than try to bind a competing listener on the same port.
	if useSystemBinaries {
		fmt.Fprintln(os.Stderr, "ycode ollama serve: --use-system-binaries set — this command is a no-op.")
		fmt.Fprintln(os.Stderr, "Run upstream `ollama serve` yourself (https://ollama.com/download), then use `ycode ollama list/pull/...` against the system daemon.")
		return nil
	}

	// Match the dataDir layout used by `ycode serve` (loadFullServeConfig
	// → ~/.agents/ycode/observability) so models live in one place
	// regardless of which entry point started ollama.
	inferenceDataDir := filepath.Join(home, ".agents", "ycode", "observability", "inference")

	// nil cfg → OllamaComponent uses defaults; OLLAMA_HOST / OLLAMA_MODELS
	// from the environment still take effect via ollama's envconfig.
	comp := inference.NewOllamaComponent(nil, inferenceDataDir)

	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := comp.Start(startCtx); err != nil {
		return err
	}

	baseURL := comp.BaseURL()
	if baseURL == "" {
		baseURL = inference.DefaultOllamaURL()
	}
	fmt.Printf("Ollama server listening on %s\n", baseURL)
	fmt.Println("Press Ctrl-C to stop.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		fmt.Println("\nShutting down...")
	case <-ctx.Done():
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	return comp.Stop(stopCtx)
}

func newOllamaPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull MODEL",
		Short: "Download a model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ollm.NewClient(ollamaBaseURL())
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
			defer cancel()
			return client.Pull(ctx, args[0], func(p ollm.PullProgress) {
				if p.Total > 0 {
					fmt.Fprintf(os.Stderr, "\r%s: %d/%d", p.Status, p.Completed, p.Total)
				} else if p.Status != "" {
					fmt.Fprintf(os.Stderr, "\r%s", p.Status)
				}
			})
		},
	}
}

func newOllamaListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List local models",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ollm.NewClient(ollamaBaseURL())
			if err != nil {
				return err
			}
			models, err := client.List(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Printf("%-40s %-20s %s\n", "NAME", "SIZE", "MODIFIED")
			for _, m := range models {
				fmt.Printf("%-40s %-20d %s\n", m.Name, m.Size, m.ModifiedAt)
			}
			return nil
		},
	}
}

func newOllamaRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm MODEL",
		Aliases: []string{"remove", "delete"},
		Short:   "Remove a model",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ollm.NewClient(ollamaBaseURL())
			if err != nil {
				return err
			}
			return client.Delete(cmd.Context(), args[0])
		},
	}
}

func newOllamaPsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ps",
		Short: "List running models",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := ollamaGet(cmd.Context(), "/api/ps")
			if err != nil {
				return err
			}
			fmt.Println(string(body))
			return nil
		},
	}
}

func newOllamaShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show MODEL",
		Short: "Show model metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := ollamaPostJSON(cmd.Context(), "/api/show", map[string]string{"model": args[0]})
			if err != nil {
				return err
			}
			fmt.Println(string(body))
			return nil
		},
	}
}

func newOllamaRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run MODEL [PROMPT...]",
		Short: "Run a model — interactive REPL when no PROMPT, one-shot otherwise",
		Long: `Maps to ycode itself with the chosen ollama model as the default:

  ollama run MODEL              → ycode --model MODEL    (interactive)
  ollama run MODEL "say hi"     → ycode prompt --model MODEL "say hi"

Model identifiers with a colon (e.g. qwen2.5:0.5b, llama3.2:3b) are
recognized as ollama-local and routed through the embedded server
automatically — no extra env-var fiddling.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			model := args[0]
			rest := args[1:]
			if len(rest) == 0 {
				return execYcode([]string{"--model", model})
			}
			return execYcode([]string{"prompt", "--print", "--model", model, strings.Join(rest, " ")})
		},
	}
	cmd.DisableFlagParsing = true
	return cmd
}

func newOllamaVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show ycode version (in lieu of ollama version)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return execYcode([]string{"version"})
		},
	}
}

// --- helpers ---

// execYcode re-execs the current binary with new arguments. Used for
// verbs that delegate to other top-level ycode subcommands (serve,
// prompt, the REPL). syscall.Exec replaces the process so PID,
// signal handling, and parent expectations stay intact.
func execYcode(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate ycode binary: %w", err)
	}
	argv := append([]string{exe}, args...)
	if err := syscall.Exec(exe, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec %s: %w", exe, err)
	}
	return nil // unreachable on success
}

func ollamaGet(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", ollamaBaseURL()+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama %s: HTTP %d: %s", path, resp.StatusCode, body)
	}
	return body, nil
}

func ollamaPostJSON(ctx context.Context, path string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", ollamaBaseURL()+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama %s: HTTP %d: %s", path, resp.StatusCode, out)
	}
	return out, nil
}
