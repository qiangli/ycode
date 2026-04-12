//go:build integration

package integration

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
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
		port := "58080"
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
