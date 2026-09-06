package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
)

// newConfigCmd is deliberately read-only. agent.yaml is the sole authored
// configuration; the CLI only displays the strict compiled representation.
func newConfigCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect the compiled agent.yaml harness",
		Long: `Inspect ycode's strict compiled harness. agent.yaml is the single
source of truth; imperative set/unset commands are intentionally unavailable.`,
	}
	cmd.PersistentFlags().StringVarP(&file, "file", "f", "agent.yaml", "harness configuration file")
	cmd.AddCommand(newConfigShowCmd(&file), newConfigPathCmd(&file), newConfigGetCmd(&file))
	return cmd
}

func loadHarness(file string) (*harnessspec.Document, error) {
	return harnessspec.Load(file)
}

func newConfigShowCmd(file *string) *cobra.Command {
	return &cobra.Command{
		Use: "show", Short: "Print the strict compiled harness as JSON", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := loadHarness(*file)
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(append(data, '\n'))
			return err
		},
	}
}

func newConfigPathCmd(file *string) *cobra.Command {
	return &cobra.Command{
		Use: "path", Short: "Print the agent.yaml path after successful compilation", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := loadHarness(*file)
			if err != nil {
				return err
			}
			path, err := filepath.Abs(doc.Source)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
			return err
		},
	}
}

func newConfigGetCmd(file *string) *cobra.Command {
	return &cobra.Command{
		Use: "get <key>", Short: "Print one compiled field (dot-separated)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := loadHarness(*file)
			if err != nil {
				return err
			}
			data, err := json.Marshal(doc)
			if err != nil {
				return err
			}
			var values map[string]any
			if err := json.Unmarshal(data, &values); err != nil {
				return err
			}
			value, ok := getDotted(values, args[0])
			if !ok {
				return fmt.Errorf("compiled harness key not found: %s", args[0])
			}
			if text, ok := value.(string); ok {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), text)
				return err
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return err
		},
	}
}

func getDotted(values map[string]any, key string) (any, bool) {
	var current any = values
	for _, part := range strings.Split(key, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
