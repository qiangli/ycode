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

func newModelPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull <model>",
		Short: "Pull a model (e.g. llama3.2:3b or hf://bartowski/Llama-3-8B-GGUF/file.gguf)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			model := args[0]
			ctx := context.Background()

			if strings.HasPrefix(model, "hf://") {
				return pullFromHuggingFace(ctx, model)
			}

			// Default: pull from Ollama registry via the running Ollama server.
			fmt.Printf("Pulling %s from Ollama registry...\n", model)
			fmt.Println("Requires a running Ollama server (ycode serve with inference enabled, or standalone ollama).")
			fmt.Println("Use: OLLAMA_HOST=<host> ollama pull", model)
			return nil
		},
	}
}

func pullFromHuggingFace(ctx context.Context, ref string) error {
	repo, filename, err := inference.ParseHFRef(ref)
	if err != nil {
		return err
	}

	if filename == "" {
		return fmt.Errorf("specify a GGUF file: hf://%s/<filename>.gguf", repo)
	}

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

	fmt.Printf("\nDownloaded to: %s\n", localPath)
	fmt.Printf("Modelfile:\n  %s\n", inference.GenerateModelfile(localPath))
	fmt.Println("To import into Ollama: ollama create <name> -f <modelfile>")
	return nil
}

func newModelListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List locally available models",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Requires a running Ollama server. Use: ollama list")
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
			fmt.Printf("Requires a running Ollama server. Use: ollama rm %s\n", args[0])
			return nil
		},
	}
}
