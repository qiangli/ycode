package main

import (
	"fmt"

	"github.com/spf13/cobra"

	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
)

func newHarnessValidateCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate and compile an agent.yaml harness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := harnessspec.Load(file)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "valid: %s (%d agents, %d pipelines)\n", doc.Metadata.Name, len(doc.Agents), len(doc.Pipelines))
			return err
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "agent.yaml", "harness configuration file")
	return cmd
}

func newHarnessSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the agent.yaml JSON Schema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := harnessspec.JSONSchema()
			if err != nil {
				return err
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout())
			return err
		},
	}
}

func init() {
	rootCmd.AddCommand(newHarnessValidateCmd(), newHarnessSchemaCmd())
}
