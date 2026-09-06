package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	coreacp "github.com/qiangli/coreutils/pkg/acp"
	harnessacp "github.com/qiangli/ycode/internal/harness/acp"
	public "github.com/qiangli/ycode/pkg/ycode"
)

type fakeHarness struct {
	mu       sync.Mutex
	runs     []public.RunRequest
	forks    []public.ForkRequest
	payloads map[string][]byte
	closed   bool
}

func (a *fakeHarness) Fork(_ context.Context, request public.ForkRequest) (<-chan public.Event, error) {
	a.mu.Lock()
	a.forks = append(a.forks, request)
	sequence := request.AtSequence + 1
	a.mu.Unlock()
	out := make(chan public.Event, 1)
	out <- public.Event{Sequence: sequence, SessionID: request.SessionID, RunID: request.RunID, Type: "session.forked"}
	close(out)
	return out, nil
}

func (a *fakeHarness) Run(_ context.Context, request public.RunRequest) (<-chan public.Event, error) {
	a.mu.Lock()
	a.runs = append(a.runs, request)
	a.mu.Unlock()
	ref := "sha256:output"
	a.payloads[ref] = []byte("answer:" + request.RunID)
	data, _ := json.Marshal(map[string]any{"deliveries": []map[string]any{{"payload_ref": ref}}})
	out := make(chan public.Event, 1)
	out <- public.Event{Sequence: uint64(len(a.runs)), SessionID: request.SessionID, RunID: request.RunID, Type: "output.emitted", Data: data}
	close(out)
	return out, nil
}
func (a *fakeHarness) Payload(ref string) ([]byte, error) { return a.payloads[ref], nil }
func (a *fakeHarness) Close() error                       { a.closed = true; return nil }

func TestACPRunnerReusesHarnessAndPersistsSessionAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, err := harnessacp.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	created := 0
	app := &fakeHarness{payloads: map[string][]byte{}}
	factory := func(string) (harnessApp, error) { created++; return app, nil }
	runner := newACPRunner(factory, io.Discard, store)
	session, err := runner.NewSession(context.Background(), filepath.Clean(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"first", "second"} {
		response, err := runner.Run(context.Background(), coreacp.TurnRequest{SessionID: session.ID, Cwd: session.Cwd, Prompt: []coreacp.ContentBlock{coreacp.TextBlock(prompt)}})
		if err != nil {
			t.Fatal(err)
		}
		if response.StopReason != coreacp.StopReasonEndTurn || !strings.HasPrefix(response.Text, "answer:") {
			t.Fatalf("response=%#v", response)
		}
	}
	if created != 1 || len(app.runs) != 2 {
		t.Fatalf("created=%d runs=%d", created, len(app.runs))
	}
	_ = runner.Close()
	reopened, err := harnessacp.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	runner2 := newACPRunner(factory, io.Discard, reopened)
	if _, err := runner2.ResumeSession(context.Background(), session.ID, session.Cwd); err != nil {
		t.Fatal(err)
	}
	if _, err := runner2.Run(context.Background(), coreacp.TurnRequest{SessionID: session.ID, Cwd: session.Cwd, Prompt: []coreacp.ContentBlock{coreacp.TextBlock("third")}}); err != nil {
		t.Fatal(err)
	}
	if got := app.runs[2].RunID; !strings.HasSuffix(got, "00000003") {
		t.Fatalf("restart run id=%q", got)
	}
}

func TestACPForkPersistsLineageAndIndependentTurnCounter(t *testing.T) {
	store, err := harnessacp.Open(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &fakeHarness{payloads: map[string][]byte{}}
	runner := newACPRunner(func(string) (harnessApp, error) { return app, nil }, io.Discard, store)
	cwd := filepath.Clean(t.TempDir())
	parent, err := runner.NewSession(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), coreacp.TurnRequest{SessionID: parent.ID, Cwd: cwd, Prompt: []coreacp.ContentBlock{coreacp.TextBlock("one")}}); err != nil {
		t.Fatal(err)
	}
	child, err := runner.ForkSession(context.Background(), parent.ID, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if child.ID == parent.ID {
		t.Fatal("fork reused parent ID")
	}
	sessions := store.List("")
	var found harnessacp.Session
	for _, value := range sessions {
		if value.ID == child.ID {
			found = value
		}
	}
	if found.ParentID != parent.ID || found.ForkSequence == 0 {
		t.Fatalf("fork=%#v", found)
	}
	if len(app.forks) != 1 || app.forks[0].ParentSessionID != parent.ID || len(app.runs) != 1 {
		t.Fatalf("forks=%#v runs=%d", app.forks, len(app.runs))
	}
	lineage := store.Lineage()
	if len(lineage) < 2 || lineage[len(lineage)-1].PreviousDigest == "" {
		t.Fatalf("lineage=%#v", lineage)
	}
}

func TestACPRunnerCancellationDoesNotInventOutput(t *testing.T) {
	store := harnessacp.NewMemory()
	blocking := &cancelHarness{}
	runner := newACPRunner(func(string) (harnessApp, error) { return blocking, nil }, io.Discard, store)
	cwd := filepath.Clean(t.TempDir())
	session, _ := runner.NewSession(context.Background(), cwd)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err := runner.Run(ctx, coreacp.TurnRequest{SessionID: session.ID, Cwd: cwd, Prompt: []coreacp.ContentBlock{coreacp.TextBlock("stop")}})
	if err == nil || response.StopReason != coreacp.StopReasonCancelled {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

type cancelHarness struct{}

func (*cancelHarness) Run(ctx context.Context, _ public.RunRequest) (<-chan public.Event, error) {
	out := make(chan public.Event)
	go func() { <-ctx.Done(); close(out) }()
	return out, nil
}
func (*cancelHarness) Fork(context.Context, public.ForkRequest) (<-chan public.Event, error) {
	return nil, errors.New("unused")
}
func (*cancelHarness) Payload(string) ([]byte, error) { return nil, nil }
func (*cancelHarness) Close() error                   { return nil }

func TestServeACPHandshakeNegotiatesProtocolV1(t *testing.T) {
	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}` + "\n"
	output := serveACPExchange(t, request, func(string) (harnessApp, error) { t.Fatal("initialize constructed harness"); return nil, nil })
	var response struct {
		Result struct {
			ProtocolVersion   int `json:"protocolVersion"`
			AgentCapabilities struct {
				LoadSession bool `json:"loadSession"`
			} `json:"agentCapabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result.ProtocolVersion != 1 || !response.Result.AgentCapabilities.LoadSession {
		t.Fatalf("response=%s", output)
	}
}

func serveACPExchange(t *testing.T, request string, factory acpAppFactory) []byte {
	t.Helper()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- serveACP(inputReader, outputWriter, io.Discard, factory) }()
	if _, err := io.WriteString(inputWriter, request); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(outputReader).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	_ = inputWriter.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return line
}
