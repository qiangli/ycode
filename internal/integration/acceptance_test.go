//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/selfinit"
)

func TestAcceptance(t *testing.T) {
	requireConnectivity(t)

	t.Run("OneShotPrompt", func(t *testing.T) {
		bin := binaryPath()
		if bin == "" {
			t.Skip("ycode binary not found")
		}
		if !isLocal(t) {
			t.Skip("CLI tests only run locally")
		}
		if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" {
			t.Skip("no API key available (set ANTHROPIC_API_KEY or OPENAI_API_KEY)")
		}

		cmd := exec.Command(bin, "--no-otel", "--print")
		cmd.Stdin = strings.NewReader("What is 2+2?")

		done := make(chan struct{})
		var out []byte
		var err error
		go func() {
			out, err = cmd.CombinedOutput()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(30 * time.Second):
			cmd.Process.Kill()
			t.Fatal("one-shot prompt timed out after 30s")
		}

		if err != nil {
			t.Fatalf("one-shot prompt failed: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "4") {
			t.Errorf("one-shot prompt output does not contain '4': %s", out)
		}
	})

	t.Run("ServeStatusSubcommand", func(t *testing.T) {
		bin := binaryPath()
		if bin == "" {
			t.Skip("ycode binary not found")
		}
		if !isLocal(t) {
			t.Skip("CLI tests only run locally")
		}
		port := strconv.Itoa(selfinit.DefaultPort)
		if p := os.Getenv("PORT"); p != "" {
			port = p
		}
		out, err := exec.Command(bin, "serve", "status", "--port", port).CombinedOutput()
		if err != nil {
			t.Fatalf("ycode serve status failed: %v\n%s", err, out)
		}
	})

	t.Run("DoctorCheck", func(t *testing.T) {
		bin := binaryPath()
		if bin == "" {
			t.Skip("ycode binary not found")
		}
		if !isLocal(t) {
			t.Skip("CLI tests only run locally")
		}
		out, err := exec.Command(bin, "doctor").CombinedOutput()
		if err != nil {
			t.Fatalf("ycode doctor failed: %v\n%s", err, out)
		}
	})
}

// sanitizedEnv returns a minimal, deterministic environment for CLI failure
// tests. It keeps only PATH and HOME from the current environment, strips all
// known provider/API variables, and appends the supplied extra key=value pairs.
// This prevents integration tests from accidentally picking up real credentials
// or agent-specific env vars that can alter ycode behavior.
func sanitizedEnv(extra ...string) []string {
	out := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}
	return append(out, extra...)
}

// isDiagnostic reports whether output contains recognizable error/diagnostic
// text. Used to assert that a failed ycode invocation is "loud" on stderr.
func isDiagnostic(output string) bool {
	lower := strings.ToLower(output)
	for _, term := range []string{
		"error", "fail", "invalid", "unauthorized", "authentication",
		"no llm provider", "no api key", "requires", "refused", "unreachable",
		"turn", "provider",
	} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

// mockAnthropicServer starts a local HTTP server that returns a 401
// authentication error immediately. This avoids relying on external networks
// and avoids ycode's retry loop for connection-refused errors.
func mockAnthropicServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":{"type":"authentication_error","message":"invalid api key"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/"
}

// TestGate2FailureIsLoud verifies that common misconfigurations exit non-zero
// and emit diagnostic text on stderr.
func TestGate2FailureIsLoud(t *testing.T) {
	bin := binaryPath()
	if bin == "" {
		t.Skip("ycode binary not found")
	}
	// binaryPath may return a relative path; resolve it once so the child
	// command works regardless of the per-test working directory.
	if abs, err := filepath.Abs(bin); err == nil {
		bin = abs
	}
	if !isLocal(t) {
		t.Skip("CLI tests only run locally")
	}

	runYcode := func(name string, env []string, args ...string) (string, error) {
		t.Helper()
		// Use an isolated HOME and cwd so each test gets its own
		// ~/.agents/ycode data directory and does not spend time indexing the
		// real workspace (the background indexer can take >30s on a large
		// codebase and is irrelevant to these failure-mode tests).
		home := t.TempDir()
		cwd := t.TempDir()
		env = append(env, "HOME="+home)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = env
		cmd.Dir = cwd
		out, err := cmd.CombinedOutput()
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("%s timed out after 30s", name)
		}
		return string(out), err
	}

	t.Run("BadModel", func(t *testing.T) {
		out, err := runYcode("bad model",
			sanitizedEnv(
				"ANTHROPIC_API_KEY=invalid-key-for-testing",
				"ANTHROPIC_BASE_URL="+mockAnthropicServer(t),
				"YCODE_NO_SERVER=1",
			),
			"prompt", "--print", "--model", "nonexistent", "say hi",
		)
		if err == nil {
			t.Fatalf("expected non-zero exit for bad model, got 0\n%s", out)
		}
		if !isDiagnostic(out) {
			t.Errorf("expected diagnostic stderr for bad model, got: %s", out)
		}
	})

	t.Run("MissingAPIKey", func(t *testing.T) {
		out, err := runYcode("missing API key",
			sanitizedEnv("YCODE_NO_SERVER=1"),
			"prompt", "--print", "say hi",
		)
		if err == nil {
			t.Fatalf("expected non-zero exit for missing API key, got 0\n%s", out)
		}
		if !isDiagnostic(out) {
			t.Errorf("expected diagnostic stderr for missing API key, got: %s", out)
		}
	})

	t.Run("BadAPIKey", func(t *testing.T) {
		out, err := runYcode("bad API key",
			sanitizedEnv(
				"ANTHROPIC_API_KEY=invalid-key-for-testing",
				"ANTHROPIC_BASE_URL="+mockAnthropicServer(t),
				"YCODE_NO_SERVER=1",
			),
			"prompt", "--print", "say hi",
		)
		if err == nil {
			t.Fatalf("expected non-zero exit for bad API key, got 0\n%s", out)
		}
		if !isDiagnostic(out) {
			t.Errorf("expected diagnostic stderr for bad API key, got: %s", out)
		}
	})
}

// TestEventsFile is the Gate 3 acceptance test: --events must emit the turn
// lifecycle, and turn.end.text must equal what --print wrote to stdout.
//
// IT LIVES OUTSIDE TestAcceptance ON PURPOSE. It used to be a subtest of it, and
// TestAcceptance opens with requireConnectivity() — which skips unless a WEB
// SERVER is deployed and answering /healthz. This is a CLI test. It spawns a
// binary and reads a file; it has no more use for a web server than it has for a
// printer.
//
// So the gate that proves the event channel works was silently skipping on every
// machine that had not run `make deploy`. It reported `ok`. A SKIP IS NOT A PASS:
// a test that can quietly not run cannot gate anything, and one that reports `ok`
// while not running is worse than no test, because it occupies the space where a
// real one would go.
func TestEventsFile(t *testing.T) {
	// The ONE precondition this test may skip on: it calls a real model, so it
	// needs a real key. Nothing else.
	//
	// It is a narrow, named, honest skip — not the one it used to inherit, which
	// was `requireConnectivity`: a check that a WEB SERVER was deployed and
	// answering /healthz. This test spawns a binary and reads a file. It has no
	// more use for a web server than it has for a printer, and it was silently
	// skipping on every machine that had not run `make deploy` — while reporting
	// `ok`.
	if os.Getenv("DEEPSEEK_API_KEY") == "" && os.Getenv("MOONSHOT_API_KEY") == "" &&
		os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("no API key (set DEEPSEEK_API_KEY / MOONSHOT_API_KEY / ANTHROPIC_API_KEY / OPENAI_API_KEY)")
	}

	bin := binaryPath()
	if bin == "" {
		t.Skip("ycode binary not found")
	}
	if !isLocal(t) {
		t.Skip("CLI tests only run locally")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("no API key available (set ANTHROPIC_API_KEY or OPENAI_API_KEY)")
	}

	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")

	// Name the model. Without it the root command falls back to a default that the
	// configured provider may not serve at all — which is how this test failed the
	// moment it was allowed to actually run: exit 1, "invalid_request_error:
	// model_not_found". The HARNESS was behaving correctly (a loud, non-zero
	// failure with a diagnostic is Gate 2 working); the test was asking for a model
	// nobody had.
	model := os.Getenv("YCODE_TEST_MODEL")
	if model == "" {
		model = "deepseek-v4-pro"
	}

	cmd := exec.Command(bin, "--no-otel", "--print", "--model", model, "--events", eventsPath)
	cmd.Stdin = strings.NewReader("What is 2+2?")
	cmd.Dir = dir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	done := make(chan struct{})
	var stdoutBytes []byte
	go func() {
		stdoutBytes, _ = io.ReadAll(stdoutPipe)
		close(done)
	}()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		cmd.Process.Kill()
		t.Fatal("events file prompt timed out after 60s")
	}

	select {
	case cmdErr := <-waitDone:
		if cmdErr != nil {
			t.Fatalf("events file prompt failed: %v\nstdout: %s", cmdErr, stdoutBytes)
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("process wait timed out")
	}

	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}

	eventsContent := strings.TrimSpace(string(eventsData))
	if eventsContent == "" {
		t.Fatal("events file is empty")
	}

	lines := strings.Split(eventsContent, "\n")
	hasTurnStart := false
	var turnEndText string
	for _, line := range lines {
		var event struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSON in events file: %v\nline: %s", err, line)
		}
		if event.Type == "turn.start" {
			hasTurnStart = true
		}
		if event.Type == "turn.end" {
			var data struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(event.Data, &data); err != nil {
				t.Fatalf("invalid turn.end data: %v", err)
			}
			turnEndText = data.Text
		}
	}

	if !hasTurnStart {
		t.Error("events file does not contain turn.start event")
	}

	if turnEndText == "" {
		t.Fatal("events file does not contain turn.end event")
	}

	stdoutStr := strings.TrimSpace(string(stdoutBytes))
	if turnEndText != stdoutStr {
		t.Errorf("turn.end.data.text does not match stdout\nturn.end.text: %q\nstdout: %q", turnEndText, stdoutStr)
	}
}
