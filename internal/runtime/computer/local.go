package computer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/qiangli/coreutils/pkg/recommend"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/fileops"
	netutil "github.com/qiangli/ycode/internal/runtime/net"
	"github.com/qiangli/ycode/internal/runtime/vfs"
)

// LocalComputer is the in-process Computer. It composes the
// existing bash.Executor, vfs.VFS, fileops.*, and a single shared
// *http.Client; every agent-driven shell / fs / web call is funneled
// through it.
type LocalComputer struct {
	exec  bash.Executor // optional; nil → direct host execution
	vfs   *vfs.VFS
	httpc *http.Client
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

// Close releases any resources held by surfaces.
func (c *LocalComputer) Close() error {
	return nil
}

// ----- Shell ---------------------------------------------------------------

type localShell struct{ c *LocalComputer }

func (s *localShell) Run(ctx context.Context, p bash.ExecParams) (*bash.ExecResult, error) {
	binary := firstWord(p.Command)
	ctx, span, finish := startSpan(ctx, s.c.Name(), "shell", "run",
		AttrCmdBinary.String(binary),
		AttrCmdLen.Int(len(p.Command)),
		AttrCmdTimeout.Int(p.Timeout),
	)

	// Fast path: try the in-process builtin dispatcher. Only
	// trivial commands (single SimpleCommand, no pipes /
	// redirections / assignments) are eligible; anything else falls
	// through to the real bash.Executor. The forked= attribute on
	// the span lets us quantify fork-avoidance from traces.
	if res, ok := tryBuiltin(ctx, p); ok {
		span.SetAttributes(
			AttrForked.Bool(false),
			AttrExitCode.Int(res.ExitCode),
		)
		finish(nil)
		recommendOnNotFound(res, p.WorkDir)
		return res, nil
	}

	span.SetAttributes(AttrForked.Bool(true))
	res, err := bash.ExecuteWith(ctx, s.c.exec, p)
	if res != nil {
		span.SetAttributes(AttrExitCode.Int(res.ExitCode))
	}
	finish(err)
	recommendOnNotFound(res, p.WorkDir)
	return res, err
}

// recommendOnNotFound appends a "did you mean" note when a command failed on a
// not-found target. It runs at this convergence point so it covers BOTH the
// in-process builtin fast-path (cat/ls/head/tail — where the agent most often
// mistypes a filename) and the forked path. The LLM can't see the filesystem;
// bashy lists what is actually there and recommends the likely intent.
// Surfaced only — never substituted.
func recommendOnNotFound(res *bash.ExecResult, workDir string) {
	if res == nil || res.ExitCode == 0 || !recommend.Enabled() {
		return
	}
	for _, missing := range recommend.NotFoundTargets(res.Stderr) {
		recs := recommend.Recommend(missing, candidateFiles(workDir, missing), 3)
		if note := recommend.Note(missing, recs); note != "" {
			res.Stderr += note
		}
	}
}

// candidateFiles lists the real entries in the directory where `missing` was
// expected (relative to workDir) plus the workDir root — bashy's ground truth.
// Bounded; never recursive.
func candidateFiles(workDir, missing string) []string {
	if workDir == "" {
		workDir = "."
	}
	dirs := map[string]bool{workDir: true}
	if d := filepath.Dir(filepath.Join(workDir, missing)); d != "" {
		dirs[d] = true
	}
	seen := map[string]bool{}
	var out []string
	for dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for i, e := range entries {
			if i >= 500 {
				break
			}
			if name := e.Name(); !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

func (s *localShell) Session(ctx context.Context, opts SessionOpts) (Session, error) {
	// Pty-backed Session lands in a follow-up commit; today we
	// surface the interface but signal that it is not yet wired.
	return nil, ErrNotSupported
}

// firstWord returns the first whitespace-separated token of s, or
// "" if s is empty. Used to attribute spans by the binary the agent
// invoked without keeping the full command line as a span attribute.
func firstWord(s string) string {
	for i, r := range s {
		if r == ' ' || r == '\t' {
			return s[:i]
		}
	}
	return s
}

// ----- Files ---------------------------------------------------------------

type localFiles struct{ c *LocalComputer }

func (f *localFiles) Read(ctx context.Context, p fileops.ReadFileParams) (out string, err error) {
	abs, err := f.c.vfs.ValidatePath(ctx, p.Path)
	if err != nil {
		return "", err
	}
	p.Path = abs
	ctx, span, finish := startSpan(ctx, f.c.Name(), "files", "read", AttrFilePath.String(abs))
	out, err = fileops.ReadFile(p)
	span.SetAttributes(AttrFileBytes.Int(len(out)))
	finish(err)
	_ = ctx
	return out, err
}

func (f *localFiles) Write(ctx context.Context, p fileops.WriteFileParams) error {
	abs, err := f.c.vfs.ValidatePath(ctx, p.Path)
	if err != nil {
		return err
	}
	p.Path = abs
	ctx, _, finish := startSpan(ctx, f.c.Name(), "files", "write",
		AttrFilePath.String(abs),
		AttrFileBytes.Int(len(p.Content)))
	err = fileops.WriteFile(p, "")
	finish(err)
	_ = ctx
	return err
}

func (f *localFiles) Edit(ctx context.Context, p fileops.EditFileParams) error {
	abs, err := f.c.vfs.ValidatePath(ctx, p.Path)
	if err != nil {
		return err
	}
	p.Path = abs
	ctx, _, finish := startSpan(ctx, f.c.Name(), "files", "edit",
		AttrFilePath.String(abs))
	err = fileops.EditFile(p)
	finish(err)
	_ = ctx
	return err
}

func (f *localFiles) Stat(ctx context.Context, path string) (os.FileInfo, error) {
	abs, err := f.c.vfs.ValidatePath(ctx, path)
	if err != nil {
		return nil, err
	}
	ctx, _, finish := startSpan(ctx, f.c.Name(), "files", "stat", AttrFilePath.String(abs))
	info, err := os.Stat(abs)
	finish(err)
	_ = ctx
	return info, err
}

func (f *localFiles) Glob(ctx context.Context, p fileops.GlobParams) (*fileops.GlobResult, error) {
	if p.Path != "" {
		abs, err := f.c.vfs.ValidatePath(ctx, p.Path)
		if err != nil {
			return nil, err
		}
		p.Path = abs
	}
	ctx, span, finish := startSpan(ctx, f.c.Name(), "files", "glob", AttrGlobPattern.String(p.Pattern))
	res, err := fileops.GlobSearch(p)
	if res != nil {
		span.SetAttributes(AttrMatchCount.Int(len(res.Files)))
	}
	finish(err)
	_ = ctx
	return res, err
}

func (f *localFiles) Grep(ctx context.Context, p fileops.GrepParams) (*fileops.GrepResult, error) {
	if p.Path != "" {
		abs, err := f.c.vfs.ValidatePath(ctx, p.Path)
		if err != nil {
			return nil, err
		}
		p.Path = abs
	}
	ctx, span, finish := startSpan(ctx, f.c.Name(), "files", "grep", AttrGrepPattern.String(p.Pattern))
	res, err := fileops.GrepSearch(p)
	if res != nil {
		span.SetAttributes(AttrMatchCount.Int(len(res.Matches)))
	}
	finish(err)
	_ = ctx
	return res, err
}

func (f *localFiles) ValidatePath(ctx context.Context, path string) (string, error) {
	return f.c.vfs.ValidatePath(ctx, path)
}

// ----- Web -----------------------------------------------------------------

type localWeb struct{ c *LocalComputer }

func (w *localWeb) Fetch(ctx context.Context, url string, opts FetchOpts) (res *FetchResult, err error) {
	ctx, span, finish := startSpan(ctx, w.c.Name(), "web", "fetch", AttrURL.String(url))
	defer func() {
		if res != nil {
			span.SetAttributes(
				AttrHTTPStatus.Int(res.Status),
				AttrHTTPBytes.Int(len(res.Body)),
			)
		}
		finish(err)
	}()

	if err = netutil.ValidateURL(url); err != nil {
		err = fmt.Errorf("SSRF protection: %w", err)
		return nil, err
	}
	req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if rerr != nil {
		err = fmt.Errorf("create request: %w", rerr)
		return nil, err
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = "ycode/1.0"
	}
	req.Header.Set("User-Agent", ua)

	client := w.c.httpc
	if opts.Timeout > 0 {
		callCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
		req = req.WithContext(callCtx)
	}

	resp, derr := client.Do(req)
	if derr != nil {
		err = fmt.Errorf("fetch %s: %w", url, derr)
		return nil, err
	}
	defer resp.Body.Close()

	max := opts.MaxBytes
	if max <= 0 {
		max = 1 << 20
	}
	body, rerr := io.ReadAll(io.LimitReader(resp.Body, max))
	if rerr != nil {
		err = fmt.Errorf("read response: %w", rerr)
		return nil, err
	}
	final := url
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	res = &FetchResult{
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   body,
		URL:    final,
	}
	return res, nil
}

func (w *localWeb) Do(ctx context.Context, req *http.Request) (resp *http.Response, err error) {
	urlStr := ""
	if req.URL != nil {
		urlStr = req.URL.String()
	}
	ctx, span, finish := startSpan(ctx, w.c.Name(), "web", "do", AttrURL.String(urlStr))
	defer func() {
		if resp != nil {
			span.SetAttributes(AttrHTTPStatus.Int(resp.StatusCode))
		}
		finish(err)
	}()

	if req.URL != nil {
		if err = netutil.ValidateURL(req.URL.String()); err != nil {
			err = fmt.Errorf("SSRF protection: %w", err)
			return nil, err
		}
	}
	if req.Context() == nil || req.Context() == context.Background() {
		req = req.WithContext(ctx)
	}
	resp, err = w.c.httpc.Do(req)
	return resp, err
}

// Compile-time assertions.
var (
	_ Computer = (*LocalComputer)(nil)
	_ Shell    = (*localShell)(nil)
	_ Files    = (*localFiles)(nil)
	_ Web      = (*localWeb)(nil)
)
