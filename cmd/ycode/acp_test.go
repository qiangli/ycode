package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	coreacp "github.com/qiangli/coreutils/pkg/acp"
)

type fakePromptApp struct {
	mu      sync.Mutex
	stdout  io.Writer
	prompts []string
	closed  bool
}

func (*fakePromptApp) SetPrintMode(bool) {}

func (a *fakePromptApp) SetOutput(stdout, _ io.Writer) {
	a.stdout = stdout
}

func (a *fakePromptApp) RunPrompt(_ context.Context, prompt string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prompts = append(a.prompts, prompt)
	_, err := io.WriteString(a.stdout, "answer:"+prompt)
	return err
}

func (a *fakePromptApp) Close() error {
	a.closed = true
	return nil
}

func TestACPRunnerReusesExistingPromptPathPerSession(t *testing.T) {
	var (
		created int
		app     *fakePromptApp
	)
	runner := newACPRunner(func(string) (promptApp, error) {
		created++
		app = &fakePromptApp{}
		return app, nil
	}, io.Discard)
	defer runner.Close()

	for _, blocks := range [][]coreacp.ContentBlock{
		{coreacp.TextBlock("first"), coreacp.TextBlock(" prompt")},
		{coreacp.TextBlock("second")},
	} {
		response, err := runner.Run(context.Background(), coreacp.TurnRequest{
			SessionID: "session-1",
			Cwd:       ".",
			Prompt:    blocks,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if response.StopReason != coreacp.StopReasonEndTurn {
			t.Fatalf("stop reason = %q, want end_turn", response.StopReason)
		}
		if strings.Contains(response.Text, "answer:first promptanswer:second") {
			t.Fatalf("response retained prior turn output: %q", response.Text)
		}
	}

	if created != 1 {
		t.Fatalf("created apps = %d, want 1", created)
	}
	if got := strings.Join(app.prompts, "|"); got != "first prompt|second" {
		t.Fatalf("RunPrompt calls = %q", got)
	}
}

func TestServeACPHandshakeNegotiatesProtocolV1(t *testing.T) {
	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}` + "\n"
	output := serveACPExchange(t, request, func(string) (promptApp, error) {
		t.Fatal("initialize must not construct a ycode app")
		return nil, nil
	})

	var response struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			ProtocolVersion int `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output), &response); err != nil {
		t.Fatalf("decode response %q: %v", output, err)
	}
	if response.JSONRPC != "2.0" || response.ID != 1 {
		t.Fatalf("response envelope = %#v", response)
	}
	if response.Result.ProtocolVersion != coreacp.ProtocolVersionNumber {
		t.Fatalf("protocol version = %d, want %d", response.Result.ProtocolVersion, coreacp.ProtocolVersionNumber)
	}
}

func TestServeACPRejectsUnsupportedProtocolVersion(t *testing.T) {
	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":2,"clientCapabilities":{}}}` + "\n"
	output := serveACPExchange(t, request, func(string) (promptApp, error) {
		return nil, nil
	})

	var response struct {
		Error *struct {
			Message string `json:"message"`
			Data    struct {
				Error string `json:"error"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output), &response); err != nil {
		t.Fatalf("decode response %q: %v", output, err)
	}
	if response.Error == nil || !strings.Contains(response.Error.Data.Error, "unsupported protocol version 2") {
		t.Fatalf("response error = %#v; output = %q", response.Error, output)
	}
}

func serveACPExchange(t *testing.T, request string, factory acpAppFactory) []byte {
	t.Helper()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serveACP(inputReader, outputWriter, io.Discard, factory)
	}()

	if _, err := io.WriteString(inputWriter, request); err != nil {
		t.Fatalf("write request: %v", err)
	}
	line, err := bufio.NewReader(outputReader).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatalf("close input: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serveACP: %v", err)
	}
	return line
}
