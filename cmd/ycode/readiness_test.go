package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
)

// isolate makes a credential test hermetic: an empty HOME (so on-disk OAuth
// credentials cannot answer for a provider), an empty cwd (so a project
// settings.json cannot pick the model) and no provider keys in the env.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Chdir(t.TempDir())
	for _, key := range []string{
		"DHNT_BASE_URL", "DHNT_API_KEY",
		"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL",
		"OPENAI_API_KEY", "OPENAI_BASE_URL",
		"XAI_API_KEY", "DASHSCOPE_API_KEY",
		"MOONSHOT_API_KEY", "KIMI_API_KEY", "DEEPSEEK_API_KEY",
		"ZAI_API_KEY", "ZAI_BASE_URL", "GLM_API_KEY",
		"GOOGLE_API_KEY", "GEMINI_API_KEY",
		"YCODE_MODEL",
	} {
		t.Setenv(key, "")
	}
	old := modelFlag
	t.Cleanup(func() { modelFlag = old })
	modelFlag = ""
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// TestReadinessMatchesRunPathCredential is the parity test.
//
// The interactive path (printReadinessReport) and the run path (what newApp
// hands to api.DetectProvider) must resolve the SAME credential for the SAME
// selected model. They did not: readiness had its own inline
// ANTHROPIC_API_KEY/OPENAI_API_KEY test, so `bashy coach` — which launches the
// interactive path — died at startup on a GLM binding that `ycode prompt` ran
// without complaint.
func TestReadinessMatchesRunPathCredential(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		env     map[string]string
		wantOK  bool
		wantKey string // credential the report must name
	}{
		{"glm with ZAI key", "glm-5.2", map[string]string{"ZAI_API_KEY": "zai-token"}, true, "ZAI_API_KEY"},
		{"glm with GLM key", "glm-5.2", map[string]string{"GLM_API_KEY": "glm-token"}, true, "GLM_API_KEY"},
		{"glm with no key", "glm-5.2", nil, false, "ZAI_API_KEY"},
		{"claude with anthropic key", "claude-sonnet-5", map[string]string{"ANTHROPIC_API_KEY": "sk-ant"}, true, "ANTHROPIC_API_KEY"},
		{"claude with no key", "claude-sonnet-5", nil, false, "ANTHROPIC_API_KEY"},
		{"kimi with kimi key", "kimi-k2.7-code", map[string]string{"KIMI_API_KEY": "kk"}, true, "KIMI_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			t.Setenv("YCODE_MODEL", tt.model)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			// The run path: newApp resolves the model with selectModel and
			// then authenticates with api.DetectProvider.
			cfg := &config.Config{}
			runModel := selectModel(cfg)
			providerCfg, runErr := api.DetectProvider(runModel)

			// The interactive path.
			readinessModel := selectedModel()
			out := captureStdout(t, printReadinessReport)

			if readinessModel != runModel {
				t.Errorf("readiness selected model %q, run path selected %q", readinessModel, runModel)
			}

			reportedReady := strings.Contains(out, "Readiness: ready")
			if reportedReady != tt.wantOK {
				t.Errorf("readiness ready = %v, want %v\n%s", reportedReady, tt.wantOK, out)
			}
			if (runErr == nil) != tt.wantOK {
				t.Errorf("run path ok = %v, want %v (err=%v)", runErr == nil, tt.wantOK, runErr)
			}
			if reportedReady != (runErr == nil) {
				t.Errorf("readiness and run path disagree for %q: readiness=%v run=%v\n%s",
					runModel, reportedReady, runErr == nil, out)
			}
			if !strings.Contains(out, tt.wantKey) {
				t.Errorf("report should name %s for model %q:\n%s", tt.wantKey, runModel, out)
			}
			if tt.wantOK && providerCfg == nil {
				t.Errorf("run path returned no provider config for %q", runModel)
			}
		})
	}
}

// TestReadinessReportsZaiModelReady pins the reported failure end to end: the
// interactive readiness report, with only ZAI_API_KEY set and a GLM model
// selected, must say ready.
func TestReadinessReportsZaiModelReady(t *testing.T) {
	isolate(t)
	t.Setenv("YCODE_MODEL", "glm-5.2")
	t.Setenv("ZAI_API_KEY", "zai-token")

	out := captureStdout(t, printReadinessReport)
	if !strings.Contains(out, "Readiness: ready") {
		t.Fatalf("glm-5.2 with ZAI_API_KEY set should be ready:\n%s", out)
	}
	if strings.Contains(out, "zai-token") {
		t.Fatalf("report leaked the credential:\n%s", out)
	}
}

// TestReadinessBlockedMessageNamesSelectedModelsKey covers the misdirecting
// message: with a GLM model selected it must name ZAI_API_KEY, not the
// hardcoded ANTHROPIC/OPENAI pair that sent an operator after a vault that was
// configured correctly all along.
func TestReadinessBlockedMessageNamesSelectedModelsKey(t *testing.T) {
	isolate(t)
	t.Setenv("YCODE_MODEL", "glm-5.2")

	out := captureStdout(t, printReadinessReport)
	if !strings.Contains(out, "Readiness: blocked") {
		t.Fatalf("expected blocked with no credentials:\n%s", out)
	}
	for _, want := range []string{"ZAI_API_KEY", "GLM_API_KEY", "glm-5.2"} {
		if !strings.Contains(out, want) {
			t.Errorf("report should name %s:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("report names %s, which model glm-5.2 never reads:\n%s", unwanted, out)
		}
	}
}

// A cascade selector is an agent, not a model: readiness must resolve it to the
// base model's credential, the same way the run does.
func TestReadinessResolvesCascadeSelectorLikeTheRun(t *testing.T) {
	isolate(t)
	t.Setenv("YCODE_MODEL", "ycode-cascade-x4")

	cfg := &config.Config{}
	runModel := selectModel(cfg)
	if runModel == "ycode-cascade-x4" {
		t.Skip("fleet catalog does not resolve ycode-cascade-x4 in this build")
	}
	if got := selectedModel(); got != runModel {
		t.Fatalf("readiness selected %q, run path selected %q", got, runModel)
	}
	if want := api.CredentialEnvVars(runModel); len(want) > 0 {
		t.Setenv(want[0], "token")
		if check := api.CheckCredentials(runModel); !check.OK {
			t.Fatalf("cascade base %q with %s set: %s", runModel, want[0], check.Message)
		}
	}
}

// TestUnattendedShouldReportAndExit pins the launch rule that kept every
// coached session from starting: an ambient-unattended ycode with a terminal
// attached opens the steerable TUI instead of printing a report and exiting.
func TestUnattendedShouldReportAndExit(t *testing.T) {
	tests := []struct {
		name            string
		explicitFlags   bool
		stdinIsTerminal bool
		want            bool
	}{
		{"--no-interactive with a tty", true, true, true},
		{"--no-interactive without a tty", true, false, true},
		{"ambient unattended with a tty (bashy coach)", false, true, false},
		{"ambient unattended without a tty", false, false, true},
	}
	for _, tt := range tests {
		if got := unattendedShouldReportAndExit(tt.explicitFlags, tt.stdinIsTerminal); got != tt.want {
			t.Errorf("%s: unattendedShouldReportAndExit(%v, %v) = %v, want %v",
				tt.name, tt.explicitFlags, tt.stdinIsTerminal, got, tt.want)
		}
	}
}
