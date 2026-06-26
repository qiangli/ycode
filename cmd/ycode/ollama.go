package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/coreutils/external/ollama"
	"github.com/qiangli/ycode/internal/inference"
)

// newOllamaCmd constructs the ollama command using the shared coreutils/external/ollama
// package, mapping ycode-specific embedded behaviors and subcommands.
func newOllamaCmd() *cobra.Command {
	opts := ollama.CmdOptions{
		UseSystemBinaries: useSystemBinaries,
		RunEmbeddedServe: func(ctx context.Context) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("user home dir: %w", err)
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
		},
		RunDelegate: func(ctx context.Context, model string, args []string) error {
			if len(args) == 0 {
				return execYcode([]string{"--model", model})
			}
			return execYcode([]string{"prompt", "--print", "--model", model, strings.Join(args, " ")})
		},
		VersionDelegate: func(ctx context.Context) error {
			return execYcode([]string{"version"})
		},
	}

	return ollama.NewOllamaCmd(opts)
}

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
