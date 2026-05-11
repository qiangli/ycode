package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Mirror of observability_default_test.go for the four other intrinsic
// feature flags. The contract for every IsEnabled() is identical:
// nil receiver, nil Enabled, or pointer-to-true → enabled. Only an
// explicit pointer-to-false opts out. This matches the ycode
// convention that bundled services (Ollama runner, Podman sandbox,
// embedded Gitea, NATS bus, chat hub) are intrinsic infrastructure.

func TestFeatureDefaults_DefaultConfigEnablesIntrinsicServices(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Inference.IsEnabled() {
		t.Error("DefaultConfig().Inference should be enabled (intrinsic)")
	}
	if !cfg.Container.IsEnabled() {
		t.Error("DefaultConfig().Container should be enabled (intrinsic)")
	}
	if !cfg.GitServer.IsEnabled() {
		t.Error("DefaultConfig().GitServer should be enabled (intrinsic)")
	}
	if !cfg.NATS.IsEnabled() {
		t.Error("DefaultConfig().NATS should be enabled (intrinsic)")
	}
	if !cfg.Chat.IsEnabled() {
		t.Error("DefaultConfig().Chat should be enabled (intrinsic)")
	}
}

func TestFeatureDefaults_NilReceiverEnabled(t *testing.T) {
	var inf *InferenceConfig
	var cont *ContainerConfig
	var gs *GitServerConfig
	var nats *NATSConfig
	var chat *ChatConfig
	if !inf.IsEnabled() || !cont.IsEnabled() || !gs.IsEnabled() || !nats.IsEnabled() || !chat.IsEnabled() {
		t.Fatal("nil receivers should report enabled=true for every intrinsic feature")
	}
}

func TestFeatureDefaults_ExplicitFalseOptsOut(t *testing.T) {
	f := false
	if (&InferenceConfig{Enabled: &f}).IsEnabled() {
		t.Error("InferenceConfig with Enabled=&false should be disabled")
	}
	if (&ContainerConfig{Enabled: &f}).IsEnabled() {
		t.Error("ContainerConfig with Enabled=&false should be disabled")
	}
	if (&GitServerConfig{Enabled: &f}).IsEnabled() {
		t.Error("GitServerConfig with Enabled=&false should be disabled")
	}
	if (&NATSConfig{Enabled: &f}).IsEnabled() {
		t.Error("NATSConfig with Enabled=&false should be disabled")
	}
	if (&ChatConfig{Enabled: &f}).IsEnabled() {
		t.Error("ChatConfig with Enabled=&false should be disabled")
	}
}

// End-to-end via the Loader: settings.json with explicit false for
// every flag must produce a config that reports each as disabled.
func TestFeatureDefaults_OptOutViaSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{
		"inference":  {"enabled": false},
		"container":  {"enabled": false},
		"gitServer":  {"enabled": false},
		"nats":       {"enabled": false},
		"chat":       {"enabled": false}
	}`)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(dir, dir, dir)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Inference.IsEnabled() {
		t.Error("Inference should be disabled after settings.json override")
	}
	if cfg.Container.IsEnabled() {
		t.Error("Container should be disabled after settings.json override")
	}
	if cfg.GitServer.IsEnabled() {
		t.Error("GitServer should be disabled after settings.json override")
	}
	if cfg.NATS.IsEnabled() {
		t.Error("NATS should be disabled after settings.json override")
	}
	if cfg.Chat.IsEnabled() {
		t.Error("Chat should be disabled after settings.json override")
	}
}

// An absent block leaves the default-on behavior intact for every flag.
func TestFeatureDefaults_AbsentBlocksKeepDefaultOn(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{"model":"some-model"}`)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(dir, dir, dir)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.Inference.IsEnabled() {
		t.Error("Inference should remain enabled when absent")
	}
	if !cfg.Container.IsEnabled() {
		t.Error("Container should remain enabled when absent")
	}
	if !cfg.GitServer.IsEnabled() {
		t.Error("GitServer should remain enabled when absent")
	}
	if !cfg.NATS.IsEnabled() {
		t.Error("NATS should remain enabled when absent")
	}
	if !cfg.Chat.IsEnabled() {
		t.Error("Chat should remain enabled when absent")
	}
}
