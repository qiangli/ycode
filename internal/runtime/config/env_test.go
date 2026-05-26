package config

import (
	"testing"
)

func TestCamelToScreamingSnake(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Model", "MODEL"},
		{"MaxTokens", "MAX_TOKENS"},
		{"SampleRate", "SAMPLE_RATE"},
		{"HTTPOnly", "HTTP_ONLY"},
		// Consecutive acronyms can't be split without a dictionary; see
		// camelToScreamingSnake LIMITATION comment.
		{"OTLPGRPCPort", "OTLPGRPC_PORT"},
		{"OTLPHTTPPort", "OTLPHTTP_PORT"},
		{"ProjectID", "PROJECT_ID"},
		{"AutoMemoryEnabled", "AUTO_MEMORY_ENABLED"},
		{"", ""},
		{"X", "X"},
		{"WSQueryParam", "WS_QUERY_PARAM"},
	}
	for _, c := range cases {
		if got := camelToScreamingSnake(c.in); got != c.want {
			t.Errorf("%q → %q, want %q", c.in, got, c.want)
		}
	}
}

func TestApplyEnvOverridesPrimitives(t *testing.T) {
	t.Setenv("YCODE_MODEL", "claude-opus-x")
	t.Setenv("YCODE_MAX_TOKENS", "16000")
	t.Setenv("YCODE_PERMISSION_MODE", "danger-full-access")
	t.Setenv("YCODE_AUTO_MEMORY_ENABLED", "true")

	cfg := DefaultConfig()
	overrides := ApplyEnvOverrides(cfg)

	if cfg.Model != "claude-opus-x" {
		t.Errorf("Model = %q, want claude-opus-x", cfg.Model)
	}
	if cfg.MaxTokens != 16000 {
		t.Errorf("MaxTokens = %d, want 16000", cfg.MaxTokens)
	}
	if cfg.PermissionMode != "danger-full-access" {
		t.Errorf("PermissionMode = %q", cfg.PermissionMode)
	}
	if !cfg.AutoMemoryEnabled {
		t.Error("AutoMemoryEnabled not set")
	}
	if len(overrides) != 4 {
		t.Errorf("got %d overrides, want 4", len(overrides))
	}
}

func TestApplyEnvOverridesNestedStruct(t *testing.T) {
	t.Setenv("YCODE_OBSERVABILITY_SAMPLE_RATE", "0.25")
	t.Setenv("YCODE_OBSERVABILITY_PROXY_PORT", "39999")
	t.Setenv("YCODE_OBSERVABILITY_COLLECTOR_ADDR", "127.0.0.1:5555")

	cfg := DefaultConfig()
	ApplyEnvOverrides(cfg)

	if cfg.Observability == nil {
		t.Fatal("Observability still nil")
	}
	if cfg.Observability.SampleRate != 0.25 {
		t.Errorf("SampleRate = %v, want 0.25", cfg.Observability.SampleRate)
	}
	if cfg.Observability.ProxyPort != 39999 {
		t.Errorf("ProxyPort = %d, want 39999", cfg.Observability.ProxyPort)
	}
	if cfg.Observability.CollectorAddr != "127.0.0.1:5555" {
		t.Errorf("CollectorAddr = %q", cfg.Observability.CollectorAddr)
	}
}

// TestAutoAllocateNilSubStruct is safeguard #3: setting an env var
// under a nil sub-config must allocate it on the way down.
func TestAutoAllocateNilSubStruct(t *testing.T) {
	t.Setenv("YCODE_SELF_HEAL_SINK_PATH", "/tmp/test-sink.jsonl")

	cfg := &Config{} // intentionally NOT DefaultConfig() — sub-configs are nil
	if cfg.SelfHeal != nil {
		t.Fatal("test precondition: SelfHeal should start nil")
	}
	ApplyEnvOverrides(cfg)
	if cfg.SelfHeal == nil {
		t.Fatal("SelfHeal not auto-allocated")
	}
	if cfg.SelfHeal.SinkPath != "/tmp/test-sink.jsonl" {
		t.Errorf("SinkPath = %q", cfg.SelfHeal.SinkPath)
	}
}

func TestEmptyEnvIgnored(t *testing.T) {
	t.Setenv("YCODE_MODEL", "") // exported but empty → treat as unset
	cfg := DefaultConfig()
	before := cfg.Model
	ApplyEnvOverrides(cfg)
	if cfg.Model != before {
		t.Errorf("empty env should not overwrite; got %q want %q", cfg.Model, before)
	}
}

func TestBadParseSilent(t *testing.T) {
	t.Setenv("YCODE_MAX_TOKENS", "not-a-number")
	cfg := DefaultConfig()
	before := cfg.MaxTokens
	ApplyEnvOverrides(cfg)
	if cfg.MaxTokens != before {
		t.Errorf("bad parse should not change field; got %d want %d", cfg.MaxTokens, before)
	}
}

func TestCustomMapExempt(t *testing.T) {
	// Setting YCODE_CUSTOM_FOO should be a no-op — the Custom map is
	// outside the env-var schema by safeguard #5.
	t.Setenv("YCODE_CUSTOM_FOO", "bar")
	cfg := DefaultConfig()
	overrides := ApplyEnvOverrides(cfg)
	for _, o := range overrides {
		if o.ConfigPath == "Custom" {
			t.Error("Custom should not receive env-var overrides")
		}
	}
}
