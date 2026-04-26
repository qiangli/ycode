package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/inference"
)

func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage local LLM models (Ollama registry + Hugging Face)",
	}

	cmd.AddCommand(
		newModelPullCmd(),
		newModelListCmd(),
		newModelSearchCmd(),
		newModelDeleteCmd(),
	)

	return cmd
}

// ollamaURL returns the base URL of a reachable Ollama server, or empty string.
func ollamaURL(ctx context.Context) string {
	u := inference.DefaultOllamaURL()
	if inference.DetectOllamaServer(ctx, u) {
		return u
	}
	return ""
}

func newModelPullCmd() *cobra.Command {
	var name string
	var cleanup bool
	cmd := &cobra.Command{
		Use:   "pull <model>",
		Short: "Pull a model (e.g. llama3.2:3b or hf://bartowski/Llama-3-8B-GGUF/file.gguf)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			model := args[0]
			ctx := context.Background()

			if strings.HasPrefix(model, "hf://") {
				return pullFromHuggingFace(ctx, model, name, cleanup)
			}

			return pullFromOllamaRegistry(ctx, model)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Override the model name in Ollama (for HF imports)")
	cmd.Flags().BoolVar(&cleanup, "cleanup", false, "Remove the raw GGUF file after successful import into Ollama")
	return cmd
}

func pullFromOllamaRegistry(ctx context.Context, model string) error {
	base := ollamaURL(ctx)
	if base == "" {
		fmt.Printf("Pulling %s from Ollama registry...\n", model)
		fmt.Println("No running Ollama server detected.")
		fmt.Println("Use: ollama pull", model)
		return nil
	}

	fmt.Printf("Pulling %s from Ollama registry...\n", model)
	return inference.OllamaPullModel(ctx, base, model, func(status string, completed, total int64) {
		if total > 0 {
			pct := float64(completed) / float64(total) * 100
			fmt.Printf("\r  %s %.1f%%", status, pct)
		} else if status != "" {
			fmt.Printf("\r  %s", status)
		}
	})
}

func pullFromHuggingFace(ctx context.Context, ref, nameOverride string, cleanup bool) error {
	repo, filename, err := inference.ParseHFRef(ref)
	if err != nil {
		return err
	}

	if filename == "" {
		return fmt.Errorf("specify a GGUF file: hf://%s/<filename>.gguf", repo)
	}

	// Download the GGUF file.
	hf := inference.NewHFClient(inference.HFConfig{})
	fmt.Printf("Downloading %s from Hugging Face (%s)...\n", filename, repo)

	localPath, err := hf.DownloadGGUF(ctx, repo, filename, func(downloaded, total int64) {
		if total > 0 {
			pct := float64(downloaded) / float64(total) * 100
			fmt.Printf("\r  %.1f%% (%d / %d bytes)", pct, downloaded, total)
		}
	})
	if err != nil {
		return err
	}
	fmt.Println()

	// Derive or use explicit model name.
	modelName := nameOverride
	if modelName == "" {
		modelName = inference.DeriveModelName(repo, filename)
	}

	// Auto-import into Ollama if server is available.
	base := ollamaURL(ctx)
	if base == "" {
		fmt.Printf("Downloaded to: %s\n", localPath)
		fmt.Printf("No running Ollama server detected. To import manually:\n")
		fmt.Printf("  ollama create %s -f - <<< 'FROM %s'\n", modelName, localPath)
		return nil
	}

	fmt.Printf("Importing as %q into Ollama...\n", modelName)
	err = inference.ImportGGUFToOllama(ctx, base, modelName, localPath, func(status string) {
		fmt.Printf("\r  %s", status)
	})
	if err != nil {
		return fmt.Errorf("import into Ollama: %w", err)
	}

	fmt.Printf("\nModel %q is ready to use.\n", modelName)

	if cleanup {
		if err := os.Remove(localPath); err != nil {
			fmt.Printf("Warning: failed to remove raw GGUF: %v\n", err)
		} else {
			fmt.Printf("Cleaned up raw GGUF: %s\n", localPath)
		}
	}
	return nil
}

func newModelListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List locally available models",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			base := ollamaURL(ctx)
			if base == "" {
				fmt.Println("No running Ollama server detected. Use: ollama list")
				return nil
			}

			responses, err := inference.OllamaListModels(ctx, base)
			if err != nil {
				return fmt.Errorf("list models: %w", err)
			}

			if len(responses) == 0 || len(responses[0].Models) == 0 {
				fmt.Println("No models found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSIZE\tMODIFIED\tFAMILY")
			for _, resp := range responses {
				for _, m := range resp.Models {
					size := formatBytes(m.Size)
					modified := m.ModifiedAt.Format("2006-01-02 15:04")
					family := m.Details.Family
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", m.Name, size, modified, family)
				}
			}
			w.Flush()
			return nil
		},
	}
}

func newModelSearchCmd() *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search for models",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			ctx := context.Background()

			switch source {
			case "hf", "huggingface":
				return searchHuggingFace(ctx, query)
			case "ollama", "":
				fmt.Printf("Search Ollama registry at: https://ollama.com/search?q=%s\n", query)
				return nil
			default:
				return fmt.Errorf("unknown source: %s (use 'hf' or 'ollama')", source)
			}
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "Model source: hf, ollama (default: ollama)")
	return cmd
}

func searchHuggingFace(ctx context.Context, query string) error {
	hf := inference.NewHFClient(inference.HFConfig{})
	models, err := hf.Search(ctx, query, 20)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		fmt.Println("No GGUF models found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tDOWNLOADS\tLIKES")
	for _, m := range models {
		fmt.Fprintf(w, "%s\t%d\t%d\n", m.ID, m.Downloads, m.Likes)
	}
	w.Flush()
	return nil
}

func newModelDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <model>",
		Short: "Delete a local model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			model := args[0]

			base := ollamaURL(ctx)
			if base == "" {
				fmt.Printf("No running Ollama server detected. Use: ollama rm %s\n", model)
				return nil
			}

			if err := inference.OllamaDeleteModel(ctx, base, model); err != nil {
				return fmt.Errorf("delete model: %w", err)
			}

			fmt.Printf("Deleted %s\n", model)
			return nil
		},
	}
}

func formatBytes(b int64) string {
	const (
		mb = 1024 * 1024
		gb = 1024 * mb
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.0f MB", float64(b)/float64(mb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
