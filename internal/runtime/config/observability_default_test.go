package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// IsEnabled is the central read site for the observability master
// switch. Convention: ycode features are on by default; the *bool
// shape exists specifically so users can opt out by setting
// `observability.enabled: false` in settings.json.

func TestObservability_IsEnabled_DefaultIsTrue(t *testing.T) {
	// Pristine DefaultConfig — Enabled is nil → IsEnabled() returns
	// true so OTEL gRPC export fires for every ycode session out of
	// the box.
	cfg := DefaultConfig()
	if !cfg.Observability.IsEnabled() {
		t.Fatal("DefaultConfig().Observability.IsEnabled() = false; want true")
	}
}

func TestObservability_IsEnabled_NilReceiverIsTrue(t *testing.T) {
	var obs *ObservabilityConfig
	if !obs.IsEnabled() {
		t.Fatal("nil receiver should report enabled=true (default)")
	}
}

func TestObservability_IsEnabled_ExplicitFalseHonored(t *testing.T) {
	f := false
	obs := &ObservabilityConfig{Enabled: &f}
	if obs.IsEnabled() {
		t.Fatal("explicit Enabled=&false should opt out; got true")
	}
}

func TestObservability_IsEnabled_ExplicitTrueHonored(t *testing.T) {
	tr := true
	obs := &ObservabilityConfig{Enabled: &tr}
	if !obs.IsEnabled() {
		t.Fatal("explicit Enabled=&true should be enabled; got false")
	}
}

// Round-trip a settings.json containing the opt-out and confirm the
// merged config disables observability. This is the user-flow:
//
//	{ "observability": { "enabled": false } }
func TestObservability_OptOutViaSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	body := []byte(`{"observability":{"enabled":false}}`)
	if err := os.WriteFile(settings, body, 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(dir, dir, dir)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Observability.IsEnabled() {
		t.Fatal("settings.json with enabled=false should yield IsEnabled()=false")
	}

	// Sanity: the JSON survived the round-trip as a non-nil pointer
	// to false. If the unmarshal lost the distinction we'd see nil.
	if cfg.Observability.Enabled == nil {
		t.Fatal("Enabled pointer should be non-nil after explicit false; got nil")
	}
	if *cfg.Observability.Enabled {
		t.Fatal("Enabled should be false")
	}
}

// Sanity: an absent observability block leaves the default-on behavior intact.
func TestObservability_AbsentSettingsKeepsDefaultOn(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	body := []byte(`{"model":"some-model"}`)
	if err := os.WriteFile(settings, body, 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(dir, dir, dir)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Observability.IsEnabled() {
		t.Fatal("missing observability block should leave default-on")
	}
}

// JSON marshal round-trip: a nil Enabled should not appear in the
// output JSON (the omitempty tag keeps the file clean). This matters
// because `ycode config set` rewrites the file and we don't want
// users to see `"enabled": null` appear out of nowhere.
func TestObservability_NilEnabledIsOmitted(t *testing.T) {
	obs := &ObservabilityConfig{}
	data, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == `{}` {
		return // good: nothing leaks
	}
	// Confirm at least there's no "enabled" key in the output.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["enabled"]; ok {
		t.Fatalf("nil Enabled should not appear in JSON; got %s", string(data))
	}
}
