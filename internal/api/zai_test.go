package api

import (
	"strings"
	"testing"
)

// The z.ai CODING PLAN endpoint is /api/coding/paas/v4 — NOT the general
// /api/paas/v4. They differ by one path segment, and a coding-plan key is rejected
// by the general endpoint. Getting this wrong reads as "the model is broken".
func TestGLMRoutesToTheCodingPlanEndpoint(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "test-key")

	cfg, matched := detectFromModel("glm-4.6")
	if !matched {
		t.Fatal("glm-4.6 did not match any provider")
	}
	if cfg == nil {
		t.Fatal("glm-4.6 matched but produced no config despite ZAI_API_KEY being set")
	}
	if cfg.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want the value of ZAI_API_KEY", cfg.APIKey)
	}
	if !strings.Contains(cfg.BaseURL, "/api/coding/paas/v4") {
		t.Errorf("BaseURL = %q — the coding plan requires /api/coding/paas/v4, "+
			"not the general /api/paas/v4", cfg.BaseURL)
	}
}

// THE LANDMINE.
//
// envNonEmpty(a, b) does NOT mean "try env var a, then env var b". Its second
// argument is a literal DEFAULT VALUE. So envNonEmpty("ZAI_API_KEY", "GLM_API_KEY")
// returns the STRING "GLM_API_KEY" when ZAI_API_KEY is unset — and ycode would send
// that as the bearer token. The request fails auth, and it looks exactly like a
// broken model or a bad subscription.
//
// This test would have caught it. It exists because the bug was written.
func TestGLMNeverSendsAnEnvVarNameAsTheKey(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("GLM_API_KEY", "")

	if got := zaiKey(); got != "" {
		t.Fatalf("with no key set, zaiKey() = %q — a missing key must be EMPTY, never a "+
			"plausible-looking string that gets sent to the API as a bearer token", got)
	}

	// The documented alternative spelling must actually work.
	t.Setenv("GLM_API_KEY", "from-glm-var")
	if got := zaiKey(); got != "from-glm-var" {
		t.Errorf("GLM_API_KEY = %q, want it honoured as the documented alternative", got)
	}

	// And ZAI_API_KEY wins when both are set.
	t.Setenv("ZAI_API_KEY", "from-zai-var")
	if got := zaiKey(); got != "from-zai-var" {
		t.Errorf("with both set, zaiKey() = %q, want ZAI_API_KEY to win", got)
	}
}

// A GLM model with NO key must produce a clear, actionable error — not fall through
// to some other provider that will 404 on an unknown model name.
func TestGLMWithoutAKeyNamesTheKeyToSet(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("GLM_API_KEY", "")

	cfg, matched := detectFromModel("glm-4.6")
	if !matched {
		t.Fatal("glm-4.6 must match the z.ai provider even without a key, so the error can name it")
	}
	if cfg != nil {
		t.Fatal("glm-4.6 produced a config with no key set — it would authenticate as nobody")
	}
	if envKey := providerEnvKey(DetectProviderFromModel("glm-4.6")); !strings.Contains(envKey, "ZAI_API_KEY") {
		t.Errorf("providerEnvKey for a glm model = %q, want it to name ZAI_API_KEY", envKey)
	}
}

// The window drives every context threshold now. A wrong number here is a wrong
// number in compaction, trimming, and the response reserve.
func TestGLMContextWindow(t *testing.T) {
	caps := DetectCapabilities(ProviderOpenAI, "glm-4.6")
	if caps.MaxContextTokens != 200_000 {
		t.Errorf("glm-4.6 window = %d, want 200000 (docs.z.ai/guides/llm/glm-4.6)", caps.MaxContextTokens)
	}
}
