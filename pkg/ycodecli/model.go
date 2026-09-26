package ycodecli

import (
	"fmt"
	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
	"os"
)

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
