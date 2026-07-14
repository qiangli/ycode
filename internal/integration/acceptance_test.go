//go:build integration

package integration

import (
	"encoding/json"
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

	t.Run("EventsFile", func(t *testing.T) {
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

		cmd := exec.Command(bin, "--no-otel", "--print", "--events", eventsPath)
		cmd.Stdin = strings.NewReader("What is 2+2?")
		cmd.Dir = dir

		done := make(chan struct{})
		var out []byte
		var err error
		go func() {
			out, err = cmd.CombinedOutput()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(60 * time.Second):
			cmd.Process.Kill()
			t.Fatal("events file prompt timed out after 60s")
		}

		if err != nil {
			t.Fatalf("events file prompt failed: %v\n%s", err, out)
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

		stdoutStr := string(out)
		if turnEndText != stdoutStr {
			t.Errorf("turn.end.data.text does not match stdout\nturn.end.text: %q\nstdout: %q", turnEndText, stdoutStr)
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
