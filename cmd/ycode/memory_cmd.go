package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

func newMemoryCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: "memory", Short: "Inspect memory policies compiled from agent.yaml"}
	cmd.PersistentFlags().StringVarP(&file, "file", "f", "agent.yaml", "harness configuration file")
	cmd.AddCommand(newMemoryListCmd(&file), newMemoryShowCmd(&file))
	return cmd
}

func newMemoryListCmd(file *string) *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List configured memory resources", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := loadHarness(*file)
			if err != nil {
				return err
			}
			names := make([]string, 0, len(doc.Spec.Memories))
			for name := range doc.Spec.Memories {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				memory := doc.Spec.Memories[name]
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%d\n", name, memory.Provider, memory.Recall.Ranking, memory.Recall.MaxItems)
			}
			return nil
		},
	}
}

func newMemoryShowCmd(file *string) *cobra.Command {
	return &cobra.Command{
		Use: "show <name>", Short: "Show one configured memory policy", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := loadHarness(*file)
			if err != nil {
				return err
			}
			memory, ok := doc.Spec.Memories[args[0]]
			if !ok {
				return fmt.Errorf("compiled harness memory not found: %s", args[0])
			}
			data, err := json.MarshalIndent(memory, "", "  ")
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(append(data, '\n'))
			return err
		},
	}
}
