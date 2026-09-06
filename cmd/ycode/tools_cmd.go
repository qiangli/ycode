package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

type configuredTool struct {
	Name         string   `json:"name"`
	Contract     string   `json:"contract"`
	RequestType  string   `json:"requestType"`
	ResultType   string   `json:"resultType"`
	Operations   []string `json:"operations"`
	Effects      []string `json:"effectsCeiling"`
	Permission   string   `json:"permissionCeiling"`
	Preflight    string   `json:"preflight"`
	HostFallback string   `json:"hostFallback"`
}

// The compiled harness intentionally exposes one model-facing tool. Bashy's
// operations are mechanisms; workflow and approval remain YAML graph policy.
func newToolsCmd() *cobra.Command {
	var file string
	var jsonOutput bool
	cmd := &cobra.Command{Use: "tools", Short: "Inspect the sole Bashy tool compiled from agent.yaml"}
	list := &cobra.Command{
		Use: "list", Short: "Print the configured Bashy contract", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := loadHarness(file)
			if err != nil {
				return err
			}
			resource := doc.Spec.Bashy
			tool := configuredTool{
				Name: "bashy", Contract: resource.Contract, RequestType: resource.RequestType,
				ResultType: resource.ResultType, Operations: append([]string(nil), resource.Operations...),
				Effects:    append([]string(nil), resource.Execution.EffectsCeiling...),
				Permission: resource.Execution.PermissionCeiling,
				Preflight:  resource.Execution.Preflight, HostFallback: resource.Execution.HostFallback,
			}
			sort.Strings(tool.Operations)
			sort.Strings(tool.Effects)
			if jsonOutput {
				data, err := json.MarshalIndent([]configuredTool{tool}, "", "  ")
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(append(data, '\n'))
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", tool.Name, tool.Contract, tool.Permission, tool.Preflight)
			fmt.Fprintf(cmd.OutOrStdout(), "operations\t%s\n", join(tool.Operations))
			return nil
		},
	}
	cmd.PersistentFlags().StringVarP(&file, "file", "f", "agent.yaml", "harness configuration file")
	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Machine-readable JSON")
	cmd.AddCommand(list)
	return cmd
}

func join(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += ","
		}
		result += value
	}
	return result
}
