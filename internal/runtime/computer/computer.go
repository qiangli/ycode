// Package computer is the unified mediation seam for agent-driven
// shell, filesystem, and web access.
//
// The Computer interface composes existing pieces (bash.Executor,
// fileops.*, *http.Client, vfs.VFS, permission.Enforcer) into one
// auditable surface so every side-effectful operation can be
// observed, gated, and swapped (Local now; Container/Remote later).
package computer

import (
	"context"
	"errors"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/fileops"
)

// ErrNotSupported is returned when a surface is unavailable in the
// current Computer implementation (e.g. Session before the pty
// implementation lands).
var ErrNotSupported = errors.New("computer: surface not supported by this implementation")

// Computer is the agent's mediated view of the host. Every
// shell / fs / web operation flows through one of its surfaces.
type Computer interface {
	Shell() Shell
	Files() Files
	Web() Web

	// Name identifies the implementation for span attributes
	// (e.g. "local"). Container/remote impls return their own name.
	Name() string
	Close() error
}

// Shell mediates command execution. Run is one-shot; Session is
// stateful, pty-aware, and required for terminal-bench compatibility.
type Shell interface {
	Run(ctx context.Context, p bash.ExecParams) (*bash.ExecResult, error)
	Session(ctx context.Context, opts SessionOpts) (Session, error)
}

// SessionOpts configures a stateful pty-backed shell session.
type SessionOpts struct {
	WorkDir string
	Env     map[string]string
	Cols    uint16
	Rows    uint16
}

// Session is a pty-backed shell session: tmux-shaped primitives
// (SendKeys, Capture, Signal) plus durable env/cwd. The
// terminal-bench harness consumes this surface.
type Session interface {
	SendKeys(ctx context.Context, keys ...string) error
	Capture(ctx context.Context, lines int) (PaneSnapshot, error)
	WaitIdle(ctx context.Context, quiet, timeout time.Duration) error
	Signal(ctx context.Context, sig syscall.Signal) error
	Resize(ctx context.Context, cols, rows uint16) error
	Env(ctx context.Context) (map[string]string, error)
	Cwd(ctx context.Context) (string, error)
	Close(ctx context.Context) error
}

// PaneSnapshot is the result of a tmux-style capture-pane.
type PaneSnapshot struct {
	Lines     []string
	CursorRow int
	CursorCol int
}

// Files mediates filesystem operations. All paths are validated
// against allowed directories before any I/O.
type Files interface {
	Read(ctx context.Context, p fileops.ReadFileParams) (string, error)
	Write(ctx context.Context, p fileops.WriteFileParams) error
	Edit(ctx context.Context, p fileops.EditFileParams) error
	Stat(ctx context.Context, path string) (os.FileInfo, error)
	Glob(ctx context.Context, p fileops.GlobParams) (*fileops.GlobResult, error)
	Grep(ctx context.Context, p fileops.GrepParams) (*fileops.GrepResult, error)
	// ValidatePath surfaces VFS validation so callers that need an
	// absolute, boundary-checked path can get it without bypassing
	// the gateway.
	ValidatePath(ctx context.Context, path string) (string, error)
}

// Web mediates outbound HTTP issued by the agent.
type Web interface {
	// Fetch issues an HTTP GET (with SSRF validation and a redirect
	// budget) and returns the response body up to opts.MaxBytes.
	Fetch(ctx context.Context, url string, opts FetchOpts) (*FetchResult, error)
	// Do is the lower-level escape hatch; callers that need full
	// request control (headers, methods, body) use this. The shared
	// *http.Client is configured with sensible timeouts and the same
	// SSRF guard as Fetch.
	Do(ctx context.Context, req *http.Request) (*http.Response, error)
}

// FetchOpts configures Web.Fetch.
type FetchOpts struct {
	UserAgent string
	MaxBytes  int64
	Timeout   time.Duration
}

// FetchResult is the response body + status from a Web.Fetch call.
type FetchResult struct {
	Status int
	Header http.Header
	Body   []byte
	URL    string // final URL after redirects
}
