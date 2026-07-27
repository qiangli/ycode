package api

import (
	"strings"
	"testing"
)

// isolateHome points HOME at an empty dir so on-disk OAuth credentials on the
// developer's machine cannot turn a "no credential" case into a ready one.
func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
}

// TestCheckCredentialsParity is the test that was missing.
//
// Readiness (the interactive path) and the request path (`ycode prompt`,
// newApp) must agree about whether a credential exists for a model. They did
// not: readiness tested ANTHROPIC_API_KEY/OPENAI_API_KEY inline while the run
// went through DetectProvider, so a GLM session with ZAI_API_KEY set was
// "blocked" on one path and working on the other.
func TestCheckCredentialsParity(t *testing.T) {
	tests := []struct {
		name   string
		model  string
		env    map[string]string
		wantOK bool
	}{
		{"glm with ZAI key", "glm-5.2", map[string]string{"ZAI_API_KEY": "zai-token"}, true},
		{"glm with GLM key", "glm-5.2", map[string]string{"GLM_API_KEY": "glm-token"}, true},
		{"glm alias with ZAI key", "glm", map[string]string{"ZAI_API_KEY": "zai-token"}, true},
		{"glm with no key", "glm-5.2", nil, false},
		{"glm with the WRONG provider key", "glm-5.2", map[string]string{"ANTHROPIC_API_KEY": "sk-ant"}, false},
		{"claude with anthropic key", "claude-sonnet-5", map[string]string{"ANTHROPIC_API_KEY": "sk-ant"}, true},
		{"claude with no key", "claude-sonnet-5", nil, false},
		{"gpt with openai key", "gpt-5.5", map[string]string{"OPENAI_API_KEY": "sk-oai"}, true},
		{"kimi with moonshot key", "kimi-k2.7-code", map[string]string{"MOONSHOT_API_KEY": "ms"}, true},
		{"kimi with kimi key", "kimi-k2.7-code", map[string]string{"KIMI_API_KEY": "kk"}, true},
		{"deepseek with deepseek key", "deepseek-chat", map[string]string{"DEEPSEEK_API_KEY": "ds"}, true},
		{"unknown model, some key set", "mystery-1", map[string]string{"ZAI_API_KEY": "zai-token"}, true},
		{"unknown model, no key", "mystery-1", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateHome(t)
			clearEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			// The request path: what newApp / RunPrompt resolve with.
			_, err := DetectProvider(tt.model)
			runPathOK := err == nil

			// The readiness path.
			check := CheckCredentials(tt.model)

			if runPathOK != tt.wantOK {
				t.Errorf("DetectProvider(%q) ok = %v, want %v (err=%v)", tt.model, runPathOK, tt.wantOK, err)
			}
			if check.OK != tt.wantOK {
				t.Errorf("CheckCredentials(%q).OK = %v, want %v (message=%q)", tt.model, check.OK, tt.wantOK, check.Message)
			}
			if check.OK != runPathOK {
				t.Errorf("readiness and request path disagree for %q: readiness=%v request=%v (message=%q)",
					tt.model, check.OK, runPathOK, check.Message)
			}
		})
	}
}

// TestCheckCredentialsZaiReady pins the exact reported failure: a z.ai GLM
// model with only ZAI_API_KEY set must read READY, and must say so in terms of
// the credential it actually used.
func TestCheckCredentialsZaiReady(t *testing.T) {
	isolateHome(t)
	clearEnv(t)
	t.Setenv("ZAI_API_KEY", "zai-token")

	check := CheckCredentials("glm-5.2")
	if !check.OK {
		t.Fatalf("glm-5.2 with ZAI_API_KEY set: got blocked (%q), want ready", check.Message)
	}
	if check.Provider != "zai" {
		t.Errorf("Provider = %q, want %q", check.Provider, "zai")
	}
	if check.Found != "ZAI_API_KEY" {
		t.Errorf("Found = %q, want ZAI_API_KEY", check.Found)
	}
	if !strings.Contains(check.Message, "ZAI_API_KEY") || !strings.Contains(check.Message, "glm-5.2") {
		t.Errorf("message %q should name both ZAI_API_KEY and the model", check.Message)
	}
	if strings.Contains(check.Message, "zai-token") {
		t.Errorf("message %q must never contain the credential itself", check.Message)
	}
}

// TestCheckCredentialsBlockedMessageNamesSelectedModelsKey covers the second
// defect: the blocked message used to name ANTHROPIC_API_KEY and
// OPENAI_API_KEY no matter which model was selected, which sent an operator
// hunting a misconfigured vault for a credential their model never reads.
func TestCheckCredentialsBlockedMessageNamesSelectedModelsKey(t *testing.T) {
	tests := []struct {
		model    string
		wantVars []string
		notVars  []string
	}{
		{"glm-5.2", []string{"ZAI_API_KEY", "GLM_API_KEY"}, []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}},
		{"kimi-k2.7-code", []string{"MOONSHOT_API_KEY", "KIMI_API_KEY"}, []string{"ANTHROPIC_API_KEY", "ZAI_API_KEY"}},
		{"deepseek-chat", []string{"DEEPSEEK_API_KEY"}, []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}},
		{"gemini-3.1-pro", []string{"GOOGLE_API_KEY", "GEMINI_API_KEY"}, []string{"ANTHROPIC_API_KEY"}},
		{"claude-sonnet-5", []string{"ANTHROPIC_API_KEY"}, []string{"ZAI_API_KEY", "OPENAI_API_KEY"}},
		{"gpt-5.5", []string{"OPENAI_API_KEY"}, []string{"ANTHROPIC_API_KEY", "ZAI_API_KEY"}},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			isolateHome(t)
			clearEnv(t)

			check := CheckCredentials(tt.model)
			if check.OK {
				t.Fatalf("expected blocked with no credentials set, got ready: %q", check.Message)
			}
			if !strings.Contains(check.Message, tt.model) {
				t.Errorf("message %q should name the selected model %q", check.Message, tt.model)
			}
			for _, v := range tt.wantVars {
				if !strings.Contains(check.Message, v) {
					t.Errorf("message %q should name %s — the credential this model needs", check.Message, v)
				}
			}
			for _, v := range tt.notVars {
				if strings.Contains(check.Message, v) {
					t.Errorf("message %q names %s, which is irrelevant to %q", check.Message, v, tt.model)
				}
			}
			if got := CredentialEnvVars(tt.model); !equalStrings(got, tt.wantVars) {
				t.Errorf("CredentialEnvVars(%q) = %v, want %v", tt.model, got, tt.wantVars)
			}
			if !equalStrings(check.Vars, tt.wantVars) {
				t.Errorf("check.Vars = %v, want %v", check.Vars, tt.wantVars)
			}
		})
	}
}

// A model that matches no provider cannot name one key, so it must at least
// say what it looked at instead of inventing two.
func TestCheckCredentialsUnknownModelListsWhatItChecked(t *testing.T) {
	isolateHome(t)
	clearEnv(t)

	check := CheckCredentials("mystery-1")
	if check.OK {
		t.Fatalf("expected blocked, got ready: %q", check.Message)
	}
	for _, v := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "ZAI_API_KEY", "DEEPSEEK_API_KEY"} {
		if !strings.Contains(check.Message, v) {
			t.Errorf("message %q should list %s among the vars it checked", check.Message, v)
		}
	}
	if !strings.Contains(check.Message, "none set") {
		t.Errorf("message %q should say the checked vars were not set", check.Message)
	}
}

func TestProviderEnvKeyRendersEveryVar(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"glm", "ZAI_API_KEY (or GLM_API_KEY)"},
		{"zai", "ZAI_API_KEY (or GLM_API_KEY)"},
		{"kimi", "MOONSHOT_API_KEY (or KIMI_API_KEY)"},
		{"gemini", "GOOGLE_API_KEY (or GEMINI_API_KEY)"},
		{"unknown", ""},
	}
	for _, tt := range tests {
		if got := providerEnvKey(tt.provider); got != tt.want {
			t.Errorf("providerEnvKey(%q) = %q, want %q", tt.provider, got, tt.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
