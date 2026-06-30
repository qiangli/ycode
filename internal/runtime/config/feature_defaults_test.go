package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Mirror of observability_default_test.go for the remaining intrinsic
// services.

func TestFeatureDefaults_DefaultConfigEnablesLeanIntrinsicServices(t *testing.T) {
	cfg := DefaultConfig()
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
	var gs *GitServerConfig
	var nats *NATSConfig
	var chat *ChatConfig
	if !gs.IsEnabled() || !nats.IsEnabled() || !chat.IsEnabled() {
		t.Fatal("nil receivers should report enabled=true for remaining intrinsic features")
	}
}

func TestFeatureDefaults_ExplicitFalseDisables(t *testing.T) {
	f := false
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

// An absent block leaves lean defaults intact.
func TestFeatureDefaults_AbsentBlocksKeepLeanDefaults(t *testing.T) {
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
