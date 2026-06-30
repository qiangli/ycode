package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/inference"
)

func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Inspect or set model configuration",
	}
	cmd.AddCommand(
		newModelCurrentCmd(),
		newModelUseCmd(),
		newModelSearchCmd(),
	)
	return cmd
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

func newModelSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search Hugging Face for GGUF models",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return searchHuggingFace(context.Background(), args[0])
		},
	}
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
	return w.Flush()
}
