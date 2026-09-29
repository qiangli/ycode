package ycodecli

// Sprint: #323; Story: #1115; Story-ID: a7f947d09851

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeKeys is a terminal the test types into.
type fakeKeys struct {
	events  chan keyEvent
	stopped bool
	partial string
}

func newFakeKeys() *fakeKeys { return &fakeKeys{events: make(chan keyEvent, 16)} }

func (k *fakeKeys) Events() <-chan keyEvent { return k.events }
func (k *fakeKeys) Stop() ([]string, string) {
	k.stopped = true
	var lines []string
	for {
		select {
		case ev := <-k.events:
			if !ev.interrupt {
				lines = append(lines, ev.line)
			}
		default:
			return lines, k.partial
		}
	}
}

// fakeHost runs turns that block until cancelled or released, draining the
// queue at "steps" the test triggers, like the compiled loop does between
// tool batches.
type fakeHost struct {
	mu       sync.Mutex
	queue    []string
	turns    []string   // text of every submitted turn
	drained  [][]string // what each step drained
	started  chan string
	step     chan struct{} // one drain step of the running turn
	finish   chan struct{} // ends the running turn normally
	settled  int
	turnErr  error
	cancelOK bool // the running turn observed its cancellation
}

func newFakeHost() *fakeHost {
	return &fakeHost{started: make(chan string, 8), step: make(chan struct{}), finish: make(chan struct{})}
}

func (h *fakeHost) Submit(ctx context.Context, text string) error {
	h.mu.Lock()
	h.turns = append(h.turns, text)
	h.mu.Unlock()
	h.started <- text
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			h.cancelOK = true
			h.mu.Unlock()
			return ctx.Err()
		case <-h.step:
			h.mu.Lock()
			h.drained = append(h.drained, h.queue)
			h.queue = nil
			h.mu.Unlock()
		case <-h.finish:
			return h.turnErr
		}
	}
}

func (h *fakeHost) Enqueue(text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.queue = append(h.queue, text)
	return nil
}

func (h *fakeHost) TakeQueued() ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	queued := h.queue
	h.queue = nil
	return queued, nil
}

func (h *fakeHost) Settle(context.Context) bool {
	h.mu.Lock()
	h.settled++
	h.mu.Unlock()
	return true
}

func (h *fakeHost) snapshot() ([]string, [][]string, int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.turns...), append([][]string(nil), h.drained...), h.settled, h.cancelOK
}

func runOwned(t *testing.T, host *fakeHost, keys *fakeKeys, sigint chan os.Signal) (chan error, *bytes.Buffer) {
	t.Helper()
	var notices bytes.Buffer
	result := make(chan error, 1)
	opened := false
	open := func() (keySource, error) {
		if opened {
			return nil, errors.New("reopened")
		}
		opened = true
		return keys, nil
	}
	go func() { result <- runOwnedTurns(context.Background(), host, open, "first task", &notices, sigint) }()
	return result, &notices
}

func waitResult(t *testing.T, result chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("owned turns did not return")
		return nil
	}
}

func waitStarted(t *testing.T, host *fakeHost, want string) {
	t.Helper()
	select {
	case got := <-host.started:
		if got != want {
			t.Fatalf("turn started with %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("turn %q never started", want)
	}
}

// A line typed mid-turn reaches the RUNNING turn at its next step; a bare
// Enter is nothing.
func TestOwnedTurnSteersRunningTurn(t *testing.T) {
	host, keys := newFakeHost(), newFakeKeys()
	result, _ := runOwned(t, host, keys, nil)
	waitStarted(t, host, "first task")
	keys.events <- keyEvent{line: ""}
	keys.events <- keyEvent{line: "STOP: a parallel lane owns this"}
	time.Sleep(50 * time.Millisecond)
	host.step <- struct{}{}
	host.finish <- struct{}{}
	if err := waitResult(t, result); err != nil {
		t.Fatalf("owned turns: %v", err)
	}
	turns, drained, _, _ := host.snapshot()
	if len(turns) != 1 {
		t.Fatalf("turns = %q, want only the first (the steer was delivered mid-turn)", turns)
	}
	if len(drained) != 1 || strings.Join(drained[0], "|") != "STOP: a parallel lane owns this" {
		t.Fatalf("the running turn drained %q, want exactly the steer", drained)
	}
	if !keys.stopped {
		t.Fatal("the terminal was not given back")
	}
}

// A line the turn never drained (it ended first) runs next as a follow-up in
// the same session.
func TestOwnedTurnQueuesFollowUp(t *testing.T) {
	host, keys := newFakeHost(), newFakeKeys()
	result, _ := runOwned(t, host, keys, nil)
	waitStarted(t, host, "first task")
	keys.events <- keyEvent{line: "then reply STEER_A"}
	time.Sleep(50 * time.Millisecond)
	host.finish <- struct{}{}
	waitStarted(t, host, "then reply STEER_A")
	host.finish <- struct{}{}
	if err := waitResult(t, result); err != nil {
		t.Fatalf("owned turns: %v", err)
	}
	if turns, _, _, _ := host.snapshot(); len(turns) != 2 {
		t.Fatalf("turns = %q, want the first and the follow-up", turns)
	}
}

// ESC cancels the running turn (it never reaches its next step), waits for it
// to settle, and reports an interrupt the session survives.
func TestOwnedTurnESCInterrupts(t *testing.T) {
	host, keys := newFakeHost(), newFakeKeys()
	result, notices := runOwned(t, host, keys, nil)
	waitStarted(t, host, "first task")
	keys.events <- keyEvent{interrupt: true}
	err := waitResult(t, result)
	if !errors.Is(err, errTurnInterrupted) {
		t.Fatalf("err = %v, want errTurnInterrupted", err)
	}
	turns, drained, settled, cancelled := host.snapshot()
	if !cancelled || settled != 1 || len(turns) != 1 || len(drained) != 0 {
		t.Fatalf("cancelled=%v settled=%d turns=%q drained=%q", cancelled, settled, turns, drained)
	}
	if notices.Len() != 0 {
		t.Fatalf("the interrupt is reported once, as the result; notices = %q", notices.String())
	}
}

// What was typed before an interrupt is not lost: it runs after the
// interrupted turn, and the result is then that turn's, not an interrupt.
func TestOwnedTurnInterruptKeepsQueuedLines(t *testing.T) {
	host, keys := newFakeHost(), newFakeKeys()
	sigint := make(chan os.Signal, 1)
	result, _ := runOwned(t, host, keys, sigint)
	waitStarted(t, host, "first task")
	keys.events <- keyEvent{line: "STOP and write HANDOFF.md"}
	time.Sleep(50 * time.Millisecond)
	sigint <- os.Interrupt // Ctrl-C: a turn interrupt, like ESC
	waitStarted(t, host, "STOP and write HANDOFF.md")
	host.finish <- struct{}{}
	if err := waitResult(t, result); err != nil {
		t.Fatalf("owned turns: %v", err)
	}
	if _, _, settled, cancelled := host.snapshot(); !cancelled || settled != 1 {
		t.Fatalf("cancelled=%v settled=%d", cancelled, settled)
	}
}

// Lines still unread when the terminal is given back become a follow-up too.
func TestOwnedTurnStopLeftoversRunNext(t *testing.T) {
	host, keys := newFakeHost(), newFakeKeys()
	keys.partial = "pushed by ESC"
	var notices bytes.Buffer
	result := make(chan error, 1)
	opens := 0
	open := func() (keySource, error) {
		opens++
		if opens == 1 {
			return keys, nil
		}
		return newFakeKeys(), nil
	}
	go func() {
		result <- runOwnedTurns(context.Background(), host, open, "first task", &notices, nil)
	}()
	waitStarted(t, host, "first task")
	host.finish <- struct{}{}
	waitStarted(t, host, "pushed by ESC")
	host.finish <- struct{}{}
	if err := waitResult(t, result); err != nil {
		t.Fatalf("owned turns: %v", err)
	}
}

// Without a terminal to own the turn runs exactly as before.
func TestOwnedTurnFallsBackWithoutTerminal(t *testing.T) {
	host := newFakeHost()
	result := make(chan error, 1)
	open := func() (keySource, error) { return nil, errors.New("not a tty") }
	go func() {
		result <- runOwnedTurns(context.Background(), host, open, "first task", &bytes.Buffer{}, nil)
	}()
	waitStarted(t, host, "first task")
	host.finish <- struct{}{}
	if err := waitResult(t, result); err != nil {
		t.Fatalf("fallback: %v", err)
	}
}

// Ctrl-C reaches the process twice — as a SIGINT and as the end of the
// process context — in either order. Both together are one turn interrupt:
// the session's queued line still runs next.
func TestOwnedTurnSIGINTThenContextIsAnInterrupt(t *testing.T) {
	host, keys := newFakeHost(), newFakeKeys()
	sigint := make(chan os.Signal, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- runOwnedTurns(ctx, host, func() (keySource, error) { return keys, nil }, "first task", &bytes.Buffer{}, sigint)
	}()
	waitStarted(t, host, "first task")
	keys.events <- keyEvent{line: "STOP"}
	time.Sleep(50 * time.Millisecond)
	sigint <- os.Interrupt
	time.Sleep(20 * time.Millisecond)
	cancel()
	waitStarted(t, host, "STOP")
	host.finish <- struct{}{}
	if err := waitResult(t, result); err != nil {
		t.Fatalf("owned turns: %v", err)
	}
}

// The process context ending without a SIGINT (SIGTERM) ends the turns: no
// follow-up runs.
func TestOwnedTurnContextEndWithoutSIGINTTerminates(t *testing.T) {
	host, keys := newFakeHost(), newFakeKeys()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- runOwnedTurns(ctx, host, func() (keySource, error) { return keys, nil }, "first task", &bytes.Buffer{}, make(chan os.Signal))
	}()
	waitStarted(t, host, "first task")
	keys.events <- keyEvent{line: "never runs"}
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := waitResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if turns, _, settled, _ := host.snapshot(); len(turns) != 1 || settled != 1 {
		t.Fatalf("turns=%q settled=%d, want one settled turn", turns, settled)
	}
}
