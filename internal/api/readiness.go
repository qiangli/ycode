package api

import (
	"fmt"
	"os"
	"strings"
)

// CredentialCheck is the result of resolving a provider credential for one
// model. It is what a readiness/doctor report renders.
type CredentialCheck struct {
	Model    string   // model id the check ran against (after alias resolution)
	Provider string   // provider display name, once resolution succeeded
	Vars     []string // credential env vars consulted for this model, best first
	Found    string   // env var the credential came from ("" for OAuth or a base-URL route)
	OK       bool
	Message  string // one line, safe to print: never contains the credential itself
}

// CheckCredentials reports whether ycode can authenticate for model.
//
// It answers by calling DetectProvider — the SAME resolver every request path
// runs through (newApp, `ycode prompt`, pkg/ycode, the shell tool). That is the
// whole point of this function: readiness must accept exactly the credential
// set the run itself accepts, never its own hardcoded subset.
//
// The subset is not a hypothetical. Readiness used to test
// `ANTHROPIC_API_KEY != "" || OPENAI_API_KEY != ""` inline, so a session bound
// to a z.ai GLM model with ZAI_API_KEY set — which `ycode prompt` ran happily —
// was reported "blocked" on the interactive path and the process exited. The
// operator, told to set two variables their model does not use, went looking
// for a broken vault instead of a broken check.
func CheckCredentials(model string) CredentialCheck {
	resolved := ResolveModel(model)
	c := CredentialCheck{
		Model: resolved,
		Vars:  CredentialEnvVars(resolved),
	}

	cfg, err := DetectProvider(model)
	if err == nil && cfg != nil {
		c.OK = true
		c.Provider = cfg.DisplayKind()
		c.Found = envVarHolding(cfg.APIKey)
		switch {
		case c.Found != "":
			c.Message = fmt.Sprintf("%s API key found (%s)", c.Provider, c.Found)
		case cfg.BearerToken != "":
			c.Message = fmt.Sprintf("%s OAuth credentials found", c.Provider)
		default:
			c.Message = fmt.Sprintf("%s endpoint configured (no API key required)", c.Provider)
		}
		if c.Model != "" {
			c.Message += " for " + c.Model
		}
		return c
	}

	// Blocked. Name the credential THIS model needs and say what was looked at:
	// a readiness failure that misdirects costs more than one that just says no.
	provider := DetectProviderFromModel(resolved)
	c.Provider = provider
	switch {
	case len(c.Vars) > 0:
		c.Message = fmt.Sprintf("No API key for %s: set %s (checked %s — none set)",
			c.Model, providerEnvKey(provider), strings.Join(c.Vars, ", "))
		if provider == "anthropic" {
			c.Message += ", or run: ycode login"
		}
	default:
		c.Vars = genericCredentialEnvVars()
		subject := "this session"
		if c.Model != "" {
			subject = fmt.Sprintf("model %q", c.Model)
		}
		c.Message = fmt.Sprintf("No API key for %s: it matches no known provider; checked %s — none set. Run `ycode login`, or set a provider key and pick a model it serves",
			subject, strings.Join(c.Vars, ", "))
	}
	return c
}

// CredentialEnvVars returns the credential env vars ycode consults for model,
// most-preferred first. Nil when the model name implies no known provider.
func CredentialEnvVars(model string) []string {
	return providerEnvVars(DetectProviderFromModel(ResolveModel(model)))
}

// genericCredentialEnvVars lists every per-provider credential var, in the
// order DetectProvider's no-model-match ladder consults them.
func genericCredentialEnvVars() []string {
	return []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "XAI_API_KEY",
		"DASHSCOPE_API_KEY", "MOONSHOT_API_KEY", "KIMI_API_KEY",
		"DEEPSEEK_API_KEY", "ZAI_API_KEY", "GLM_API_KEY",
		"GOOGLE_API_KEY", "GEMINI_API_KEY",
	}
}

// envVarHolding reports which known credential env var currently holds key, so
// a ready report can say WHERE the credential came from. The comparison is
// in-process only; the key itself is never returned or logged.
func envVarHolding(key string) string {
	if key == "" {
		return ""
	}
	for _, name := range append([]string{"DHNT_API_KEY"}, genericCredentialEnvVars()...) {
		if os.Getenv(name) == key {
			return name
		}
	}
	return ""
}
