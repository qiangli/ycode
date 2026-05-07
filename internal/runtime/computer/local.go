package computer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/fileops"
	netutil "github.com/qiangli/ycode/internal/runtime/net"
	"github.com/qiangli/ycode/internal/runtime/vfs"
)

// LocalComputer is the in-process Computer. It composes the
// existing bash.Executor, vfs.VFS, fileops.*, and a single shared
// *http.Client; every agent-driven shell / fs / web call is funneled
// through it. Browser is left as a runtime-degrade stub here; the
// real CDP impl plugs in via WithBrowser.
type LocalComputer struct {
	exec    bash.Executor // optional; nil → direct host execution
	vfs     *vfs.VFS
	httpc   *http.Client
	browser Browser // optional; nil → ErrNotSupported
}

// LocalOption configures a LocalComputer.
type LocalOption func(*LocalComputer)

// WithExecutor overrides the default host bash executor (e.g. with a
// container-backed one).
func WithExecutor(e bash.Executor) LocalOption {
	return func(c *LocalComputer) { c.exec = e }
}

// WithHTTPClient overrides the default web HTTP client.
func WithHTTPClient(h *http.Client) LocalOption {
	return func(c *LocalComputer) { c.httpc = h }
}

// WithBrowser plugs in a Browser implementation (typically a CDP
// session). When omitted, Browser surface returns ErrNotSupported.
func WithBrowser(b Browser) LocalOption {
	return func(c *LocalComputer) { c.browser = b }
}

// NewLocal constructs a LocalComputer over the given VFS. The VFS
// is mandatory because every Files operation re-validates against
// allowed directories.
func NewLocal(v *vfs.VFS, opts ...LocalOption) *LocalComputer {
	c := &LocalComputer{
		vfs: v,
		httpc: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				if err := netutil.ValidateURL(req.URL.String()); err != nil {
					return fmt.Errorf("SSRF protection on redirect: %w", err)
				}
				return nil
			},
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name returns "local" for trace attribution.
func (c *LocalComputer) Name() string { return "local" }

// Shell returns the shell surface.
func (c *LocalComputer) Shell() Shell { return &localShell{c: c} }

// Files returns the filesystem surface.
func (c *LocalComputer) Files() Files { return &localFiles{c: c} }

// Web returns the web surface.
func (c *LocalComputer) Web() Web { return &localWeb{c: c} }

// Browser returns the browser surface; when no Browser was
// configured, returns one whose methods all yield ErrNotSupported.
func (c *LocalComputer) Browser() Browser {
	if c.browser != nil {
		return c.browser
	}
	return unsupportedBrowser{}
}

// Close releases any resources held by surfaces (notably the
// Browser if one is attached).
func (c *LocalComputer) Close() error {
	if c.browser != nil {
		return c.browser.Close(context.Background())
	}
	return nil
}

// ----- Shell ---------------------------------------------------------------

type localShell struct{ c *LocalComputer }

func (s *localShell) Run(ctx context.Context, p bash.ExecParams) (*bash.ExecResult, error) {
	return bash.ExecuteWith(ctx, s.c.exec, p)
}

func (s *localShell) Session(ctx context.Context, opts SessionOpts) (Session, error) {
	// Pty-backed Session lands in a follow-up commit; today we
	// surface the interface but signal that it is not yet wired.
	return nil, ErrNotSupported
}

// ----- Files ---------------------------------------------------------------

type localFiles struct{ c *LocalComputer }

func (f *localFiles) Read(ctx context.Context, p fileops.ReadFileParams) (string, error) {
	abs, err := f.c.vfs.ValidatePath(ctx, p.Path)
	if err != nil {
		return "", err
	}
	p.Path = abs
	return fileops.ReadFile(p)
}

func (f *localFiles) Write(ctx context.Context, p fileops.WriteFileParams) error {
	abs, err := f.c.vfs.ValidatePath(ctx, p.Path)
	if err != nil {
		return err
	}
	p.Path = abs
	return fileops.WriteFile(p, "")
}

func (f *localFiles) Edit(ctx context.Context, p fileops.EditFileParams) error {
	abs, err := f.c.vfs.ValidatePath(ctx, p.Path)
	if err != nil {
		return err
	}
	p.Path = abs
	return fileops.EditFile(p)
}

func (f *localFiles) Stat(ctx context.Context, path string) (os.FileInfo, error) {
	abs, err := f.c.vfs.ValidatePath(ctx, path)
	if err != nil {
		return nil, err
	}
	return os.Stat(abs)
}

func (f *localFiles) Glob(ctx context.Context, p fileops.GlobParams) (*fileops.GlobResult, error) {
	if p.Path != "" {
		abs, err := f.c.vfs.ValidatePath(ctx, p.Path)
		if err != nil {
			return nil, err
		}
		p.Path = abs
	}
	return fileops.GlobSearch(p)
}

func (f *localFiles) Grep(ctx context.Context, p fileops.GrepParams) (*fileops.GrepResult, error) {
	if p.Path != "" {
		abs, err := f.c.vfs.ValidatePath(ctx, p.Path)
		if err != nil {
			return nil, err
		}
		p.Path = abs
	}
	return fileops.GrepSearch(p)
}

func (f *localFiles) ValidatePath(ctx context.Context, path string) (string, error) {
	return f.c.vfs.ValidatePath(ctx, path)
}

// ----- Web -----------------------------------------------------------------

type localWeb struct{ c *LocalComputer }

func (w *localWeb) Fetch(ctx context.Context, url string, opts FetchOpts) (*FetchResult, error) {
	if err := netutil.ValidateURL(url); err != nil {
		return nil, fmt.Errorf("SSRF protection: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = "ycode/1.0"
	}
	req.Header.Set("User-Agent", ua)

	client := w.c.httpc
	if opts.Timeout > 0 {
		// Per-call timeout: use a derived context rather than mutating
		// the shared client.
		callCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
		req = req.WithContext(callCtx)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	max := opts.MaxBytes
	if max <= 0 {
		max = 1 << 20 // 1 MB default; matches existing WebFetch
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	final := url
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	return &FetchResult{
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   body,
		URL:    final,
	}, nil
}

func (w *localWeb) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req.URL != nil {
		if err := netutil.ValidateURL(req.URL.String()); err != nil {
			return nil, fmt.Errorf("SSRF protection: %w", err)
		}
	}
	if req.Context() == nil || req.Context() == context.Background() {
		req = req.WithContext(ctx)
	}
	return w.c.httpc.Do(req)
}

// ----- Browser (unsupported stub) -----------------------------------------

type unsupportedBrowser struct{}

func (unsupportedBrowser) Goto(context.Context, string) error           { return ErrNotSupported }
func (unsupportedBrowser) Click(context.Context, Selector) error        { return ErrNotSupported }
func (unsupportedBrowser) Type(context.Context, Selector, string) error { return ErrNotSupported }
func (unsupportedBrowser) Press(context.Context, string) error          { return ErrNotSupported }
func (unsupportedBrowser) Scroll(context.Context, int, int) error       { return ErrNotSupported }
func (unsupportedBrowser) Screenshot(context.Context, ShotOpts) ([]byte, error) {
	return nil, ErrNotSupported
}
func (unsupportedBrowser) Snapshot(context.Context) (PageSnapshot, error) {
	return PageSnapshot{}, ErrNotSupported
}
func (unsupportedBrowser) Eval(context.Context, string) ([]byte, error) {
	return nil, ErrNotSupported
}
func (unsupportedBrowser) WaitFor(context.Context, WaitCond) error { return ErrNotSupported }
func (unsupportedBrowser) Close(context.Context) error             { return nil }

// Compile-time assertions.
var (
	_ Computer = (*LocalComputer)(nil)
	_ Shell    = (*localShell)(nil)
	_ Files    = (*localFiles)(nil)
	_ Web      = (*localWeb)(nil)
	_ Browser  = unsupportedBrowser{}
)
