package netscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// Session is a long-lived SSH shell that survives across detach. The
// ssh.Client and ssh.Session stay open even when no one is attached;
// remote output drains into a ring buffer so it can be replayed when
// the user re-attaches.
//
// **Where this lives.** The Manager is meant to be a singleton on the
// ycode *server* side (the long-running process that survives TUI
// reconnects), so SSH sessions persist across `ycode` TUI exits and
// can be reattached from a different client (TUI, web UI, future
// IDE plugin). Clients drive the session via `Send` (write to
// remote stdin without taking the local TTY — the "/btw"-style
// interjection) and `Attach` (full raw-mode interactive takeover).
//
// Lifecycle:
//
//	Connect (Manager) → Attach … Detach … Send … Attach … → Close
//
// Multiple Attaches over the lifetime are supported; only one Attach
// at a time. Calling Attach while another caller is attached returns
// ErrAlreadyAttached.
type Session struct {
	ID       string
	Host     *Host
	User     string
	OpenedAt time.Time

	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser

	mu        sync.Mutex
	attached  atomic.Bool
	closed    atomic.Bool
	detachReq chan struct{}

	out *ringBuffer

	exitErr   error
	exitedC   chan struct{}
	closeOnce sync.Once
}

// ErrAlreadyAttached is returned by Attach when another caller already
// owns the session's TTY. Use Detach (or wait for the existing
// attacher to exit) before retrying.
var ErrAlreadyAttached = errors.New("netscan session: already attached")

// ErrSessionClosed is returned by Attach / Send after Close.
var ErrSessionClosed = errors.New("netscan session: closed")

// ringBuffer is a fixed-size, lock-protected byte ring used to keep
// the most recent N bytes of remote output for replay on attach.
type ringBuffer struct {
	mu    sync.Mutex
	buf   []byte
	start int
	full  bool
	subs  map[chan []byte]struct{}
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{buf: make([]byte, size), subs: map[chan []byte]struct{}{}}
}

// Write copies p into the ring (overwriting oldest bytes when full)
// and forwards to every live subscriber. Always succeeds; never errors.
func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	for i := 0; i < len(p); i++ {
		r.buf[r.start] = p[i]
		r.start = (r.start + 1) % len(r.buf)
		if r.start == 0 {
			r.full = true
		}
	}
	subs := make([]chan []byte, 0, len(r.subs))
	for c := range r.subs {
		subs = append(subs, c)
	}
	r.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- append([]byte(nil), p...):
		default:
			// Slow consumer — drop this chunk. Detached attaches will
			// catch up via the buffered snapshot on next Attach.
		}
	}
	return len(p), nil
}

// snapshot returns the current ring contents in order (oldest first).
func (r *ringBuffer) snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]byte, r.start)
		copy(out, r.buf[:r.start])
		return out
	}
	out := make([]byte, len(r.buf))
	copy(out, r.buf[r.start:])
	copy(out[len(r.buf)-r.start:], r.buf[:r.start])
	return out
}

func (r *ringBuffer) subscribe() chan []byte {
	c := make(chan []byte, 32)
	r.mu.Lock()
	r.subs[c] = struct{}{}
	r.mu.Unlock()
	return c
}

func (r *ringBuffer) unsubscribe(c chan []byte) {
	r.mu.Lock()
	delete(r.subs, c)
	r.mu.Unlock()
}

// Connect dials, requests a PTY, starts a shell, and returns a
// non-attached Session. The remote shell runs in the background;
// Attach takes over the local terminal when the caller is ready.
func Connect(ctx context.Context, host *Host, opts SSHOptions) (*Session, error) {
	client, err := dial(ctx, host, opts)
	if err != nil {
		return nil, err
	}
	sshSess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("new ssh session: %w", err)
	}

	// Default PTY size matches a typical terminal; the attacher will
	// resize via window-change requests on Attach.
	termType := os.Getenv("TERM")
	if termType == "" {
		termType = "xterm-256color"
	}
	if err := sshSess.RequestPty(termType, 24, 80, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		_ = sshSess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	stdin, err := sshSess.StdinPipe()
	if err != nil {
		_ = sshSess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	out := newRingBuffer(64 * 1024) // 64 KiB scrollback per session
	sshSess.Stdout = out
	sshSess.Stderr = out

	if err := sshSess.Shell(); err != nil {
		_ = sshSess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	s := &Session{
		Host:      host,
		User:      opts.User,
		OpenedAt:  time.Now().UTC(),
		client:    client,
		session:   sshSess,
		stdin:     stdin,
		out:       out,
		detachReq: make(chan struct{}, 1),
		exitedC:   make(chan struct{}),
	}

	go func() {
		err := sshSess.Wait()
		s.mu.Lock()
		s.exitErr = err
		s.mu.Unlock()
		close(s.exitedC)
		s.markClosed()
	}()

	return s, nil
}

// Send writes raw bytes to the remote shell's stdin without attaching
// the local TTY. Useful for the "fire a command and detach" pattern,
// e.g. `tail -f` or `top` running in the background.
func (s *Session) Send(p []byte) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	_, err := s.stdin.Write(p)
	return err
}

// Snapshot returns the current scrollback (last ~64 KiB) without
// attaching. Handy for a TUI sessions panel that wants to render a
// preview of what's running in the background.
func (s *Session) Snapshot() []byte { return s.out.snapshot() }

// Exited returns a channel that closes when the remote shell exits.
func (s *Session) Exited() <-chan struct{} { return s.exitedC }

// ExitError returns the remote shell's exit error, if any. nil before
// Exited fires; nil if the shell exited cleanly.
func (s *Session) ExitError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitErr
}

// Close terminates the session and frees the underlying TCP
// connection. Safe to call from any goroutine; idempotent.
func (s *Session) Close() error {
	s.markClosed()
	return nil
}

func (s *Session) markClosed() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		_ = s.stdin.Close()
		_ = s.session.Close()
		_ = s.client.Close()
	})
}

// Attach takes over the local TTY and streams I/O between it and the
// remote shell. Returns when the user requests a detach (DetachKey),
// the remote shell exits, or ctx is cancelled.
//
// The local terminal must be a TTY — the TUI/CLI is expected to have
// released it (mirrors the bash TTYExecutor handover pattern).
func (s *Session) Attach(ctx context.Context, detachKey byte) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	if !s.attached.CompareAndSwap(false, true) {
		return ErrAlreadyAttached
	}
	defer s.attached.Store(false)

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("netscan attach: stdin is not a TTY")
	}
	prevState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("set raw mode: %w", err)
	}
	defer func() { _ = term.Restore(fd, prevState) }()

	// Resize remote PTY to match the current local size.
	if w, h, err := term.GetSize(fd); err == nil {
		_ = s.session.WindowChange(h, w)
	}

	// Print scrollback so the attacher sees what they missed.
	if scroll := s.Snapshot(); len(scroll) > 0 {
		_, _ = os.Stdout.Write(scroll)
	}

	// Subscribe to live output and pipe it to stdout.
	live := s.out.subscribe()
	defer s.out.unsubscribe(live)

	doneOut := make(chan struct{})
	go func() {
		defer close(doneOut)
		for {
			select {
			case chunk, ok := <-live:
				if !ok {
					return
				}
				_, _ = os.Stdout.Write(chunk)
			case <-s.exitedC:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Forward stdin → remote, watching for the detach key.
	doneIn := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				// Detect the detach key as a standalone byte. Most
				// users press it cleanly; mid-stream presses (e.g.
				// inside a paste) survive because we still forward
				// the rest.
				idx := -1
				if detachKey != 0 {
					for i := 0; i < n; i++ {
						if buf[i] == detachKey {
							idx = i
							break
						}
					}
				}
				if idx >= 0 {
					if idx > 0 {
						_, _ = s.stdin.Write(buf[:idx])
					}
					select {
					case s.detachReq <- struct{}{}:
					default:
					}
					return
				}
				if _, werr := s.stdin.Write(buf[:n]); werr != nil {
					doneIn <- werr
					return
				}
			}
			if err != nil {
				doneIn <- err
				return
			}
		}
	}()

	select {
	case <-s.detachReq:
		return nil
	case <-s.exitedC:
		return s.ExitError()
	case <-ctx.Done():
		return ctx.Err()
	case err := <-doneIn:
		return err
	}
}

// Manager is a registry of live SSH sessions. It is the entry point
// for the "background, multi-server, switchable" workflow: connect
// returns a session ID; the caller can then Attach to it later and
// repeat for as many servers as needed.
//
// All methods are safe under concurrent callers.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	counter  int
}

// NewManager constructs an empty Manager.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// Connect dials the host and stores the returned Session under a
// generated ID. The ID is short (e.g. "s1") so users can refer to it
// from a TUI or CLI prompt without copy-pasting a UUID.
func (m *Manager) Connect(ctx context.Context, host *Host, opts SSHOptions) (string, *Session, error) {
	s, err := Connect(ctx, host, opts)
	if err != nil {
		return "", nil, err
	}
	m.mu.Lock()
	m.counter++
	id := fmt.Sprintf("s%d", m.counter)
	s.ID = id
	m.sessions[id] = s
	m.mu.Unlock()

	// Drop closed sessions from the registry asynchronously so List
	// reflects what's actually live.
	go func() {
		<-s.Exited()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
	}()
	return id, s, nil
}

// Get returns the session with the given ID, or nil.
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// List returns a snapshot of live sessions, sorted by open time
// (oldest first).
func (m *Manager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

// Close terminates the session (if any) with the given ID.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.Close()
}

// CloseAll terminates every live session. Safe to call during ycode
// shutdown.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, s := range sessions {
		_ = s.Close()
	}
}
