package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
)

func newModelCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: "model", Short: "Inspect models compiled from agent.yaml"}
	cmd.PersistentFlags().StringVarP(&file, "file", "f", "agent.yaml", "harness configuration file")
	cmd.AddCommand(newModelCurrentCmd(&file), newModelListCmd(&file))
	return cmd
}

func newModelCurrentCmd(file *string) *cobra.Command {
	return &cobra.Command{
		Use: "current", Short: "Print the default agent's first configured model", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := loadHarness(*file)
			if err != nil {
				return err
			}
			model, err := defaultHarnessModel(doc)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), model)
			return err
		},
	}
}

func defaultHarnessModel(doc *harnessspec.Document) (string, error) {
	_, model, err := defaultHarnessModelResource(doc)
	if err != nil {
		return "", err
	}
	return model.ID, nil
}

func defaultHarnessModelResource(doc *harnessspec.Document) (string, harnessspec.Model, error) {
	agentRef := doc.Spec.Runtime.DefaultAgentRef
	agent, ok := doc.Spec.Agents[agentRef]
	if !ok {
		return "", harnessspec.Model{}, fmt.Errorf("compiled harness has no default agent %q", agentRef)
	}
	route, ok := doc.Spec.Routes[agent.ModelRouteRef]
	if !ok || len(route.Attempts) == 0 {
		return "", harnessspec.Model{}, fmt.Errorf("default agent %q has no model route attempts", agentRef)
	}
	modelRef := route.Attempts[0].ModelRef
	model, ok := doc.Spec.Models[modelRef]
	if !ok {
		return "", harnessspec.Model{}, fmt.Errorf("route references unknown model %q", modelRef)
	}
	return modelRef, model, nil
}

func harnessCredentialStatus(doc *harnessspec.Document) (string, bool) {
	_, model, err := defaultHarnessModelResource(doc)
	if err != nil {
		return err.Error(), false
	}
	provider, ok := doc.Spec.Providers[model.ProviderRef]
	if !ok {
		return "configured provider is missing", false
	}
	secret := provider.Credentials.APIKey.SecretRef
	if secret.Provider != "env" {
		return fmt.Sprintf("credential configured as %s/%s", secret.Provider, secret.Name), true
	}
	if value, ok := os.LookupEnv(secret.Name); ok && value != "" {
		return fmt.Sprintf("credential found in %s", secret.Name), true
	}
	return fmt.Sprintf("credential environment variable %s is not set", secret.Name), false
}

func newModelListCmd(file *string) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use: "list", Short: "List configured model resources", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := loadHarness(*file)
			if err != nil {
				return err
			}
			names := make([]string, 0, len(doc.Spec.Models))
			for name := range doc.Spec.Models {
				names = append(names, name)
			}
			sort.Strings(names)
			if jsonOutput {
				rows := make(map[string]any, len(names))
				for _, name := range names {
					rows[name] = doc.Spec.Models[name]
				}
				data, err := json.MarshalIndent(rows, "", "  ")
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(append(data, '\n'))
				return err
			}
			for _, name := range names {
				model := doc.Spec.Models[name]
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", name, model.ID, model.ProviderRef)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Machine-readable JSON")
	return cmd
}
