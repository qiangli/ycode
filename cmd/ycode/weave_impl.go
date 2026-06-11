package main

// Minimum-viable weave subverb bodies. Backs add/list/next/start/pull/
// abandon on a per-repo JSON queue + git worktrees, with no Gitea or
// merger dependency. The shape matches the surface in weave_subverbs.go;
// the full v2 design (Gitea-backed queue, loom merger auto-merge, MCP
// collab verbs) supersedes this once N+1 group A/B lands. See
// docs/loom-v2-implementation.md for the broader plan.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/cli/weavecli"
)

type weaveQueue struct {
	NextID int64        `json:"next_id"`
	Items  []*weaveItem `json:"items"`
	// Root is the repo the queue serves, stamped on writes. Queues
	// are keyed by a path-mangled tag that can't be reversed; Root
	// lets `weave list` name nearby queues in its empty-queue hint.
	Root string `json:"root,omitempty"`
}

// weaveOtherActiveQueues scans sibling queue dirs for queues with
// non-terminal items — fuel for the "ran from the wrong directory"
// hint, the most common weave confusion in dogfooding.
func weaveOtherActiveQueues(currentDir string) []map[string]any {
	base := filepath.Dir(currentDir)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, e := range entries {
		if !e.IsDir() || filepath.Join(base, e.Name()) == currentDir {
			continue
		}
		q, err := loadWeaveQueue(filepath.Join(base, e.Name()))
		if err != nil {
			continue
		}
		active := 0
		for _, it := range q.Items {
			if !isTerminalState(it.State) {
				active++
			}
		}
		if active == 0 {
			continue
		}
		name := q.Root
		if name == "" {
			name = e.Name()
		}
		out = append(out, map[string]any{"root": name, "active": active})
	}
	return out
}

type weaveItem struct {
	ID         int64     `json:"id"`
	Title      string    `json:"title"`
	Body       string    `json:"body,omitempty"`
	Priority   string    `json:"priority,omitempty"`
	State      string    `json:"state"`
	Sandbox    string    `json:"sandbox,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Created    time.Time `json:"created"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	ExitCode   *int      `json:"exit_code,omitempty"`
	LogPath    string    `json:"log_path,omitempty"`
	// WrapperPid is the PID of the `ycode weave start` process
	// supervising this item (NOT the subagent's PID — the wrapper
	// is the session leader after auto-setsid and signals propagate
	// from there to the whole subagent process group). Set when
	// state flips to working; cleared on terminal state. Used by
	// `weave abandon` for precise SIGTERM instead of pkill-by-name.
	WrapperPid int `json:"wrapper_pid,omitempty"`
	// Stale is computed at read time by `weave list` (never
	// persisted): state is "working" but the recorded wrapper PID
	// is no longer alive — the wrapper crashed or was killed
	// outside weave's control. Resume or abandon the item.
	Stale bool `json:"stale,omitempty"`
	// CtlSock is the wrapper's per-issue control socket while the
	// subagent runs (PTY mode only). `weave say` connects here to
	// inject a line into the subagent's stdin. Set at claim time,
	// cleared on terminal state.
	CtlSock string `json:"ctl_sock,omitempty"`
}

// weaveCtlSockPath returns the per-issue control socket path,
// falling back to the temp dir when the queue-dir path would
// exceed the unix socket path limit (104 bytes on darwin).
func weaveCtlSockPath(dir string, id int64) string {
	p := filepath.Join(dir, "ctl", fmt.Sprintf("issue-%d.sock", id))
	if len(p) <= 100 {
		return p
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(dir))
	return filepath.Join(os.TempDir(), fmt.Sprintf("ycode-weave-%x-issue-%d.sock", h.Sum32(), id))
}

// Terminal states for queue items — used by `weave wait` and similar
// orchestrator-side polling. "submitted" means the subagent exited
// cleanly and its branch is ready to be merged by `weave pull`.
// "failed" means the subagent exited non-zero; the branch is left
// alone (no merge) and the user can inspect the log to decide.
func isTerminalState(s string) bool {
	switch s {
	case "submitted", "failed", "done", "abandoned":
		return true
	}
	return false
}

func weaveRepoRoot(cwd string) (string, error) {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repo (run from a clone): %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func weaveBaseBranch(root string) string {
	for _, b := range []string{"main", "master"} {
		if err := exec.Command("git", "-C", root, "rev-parse", "--verify", "refs/heads/"+b).Run(); err == nil {
			return b
		}
	}
	return "HEAD"
}

func weaveQueueDir(repoRoot string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	// Containment: the tag must NOT spell out the repo path — the
	// sandbox lives under this dir, so a path-mangled tag hands every
	// subagent the origin location (one escape decoded exactly that).
	// basename keeps the dir human-navigable; the hash disambiguates
	// same-named repos without revealing where they live.
	h := fnv.New32a()
	_, _ = h.Write([]byte(repoRoot))
	tag := fmt.Sprintf("%s-%08x", filepath.Base(repoRoot), h.Sum32())
	dir := filepath.Join(home, ".agents", "ycode", "weave", tag)
	// One-time migration from the legacy path-mangled tag so existing
	// queues (history, logs) carry over.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		r := strings.NewReplacer(string(filepath.Separator), "_", ":", "_")
		legacy := r.Replace(strings.TrimPrefix(repoRoot, string(filepath.Separator)))
		if len(legacy) > 120 {
			legacy = legacy[len(legacy)-120:]
		}
		legacyDir := filepath.Join(home, ".agents", "ycode", "weave", legacy)
		if st, err := os.Stat(legacyDir); err == nil && st.IsDir() {
			_ = os.Rename(legacyDir, dir)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func loadWeaveQueue(dir string) (*weaveQueue, error) {
	b, err := os.ReadFile(filepath.Join(dir, "queue.json"))
	if errors.Is(err, os.ErrNotExist) {
		return &weaveQueue{NextID: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var q weaveQueue
	if err := json.Unmarshal(b, &q); err != nil {
		return nil, fmt.Errorf("queue parse: %w", err)
	}
	if q.NextID == 0 {
		q.NextID = 1
	}
	return &q, nil
}

func saveWeaveQueue(dir string, q *weaveQueue) error {
	if q.Root == "" {
		// Best-effort back-stamp for queues created before Root
		// existed; saveWeaveQueue callers all run from the repo.
		if cwd, err := os.Getwd(); err == nil {
			if root, err := weaveRepoRoot(cwd); err == nil {
				q.Root = root
			}
		}
	}
	path := filepath.Join(dir, "queue.json")
	tmp := path + ".tmp"
	b, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// weaveStartedCol renders the subagent's start time for the list
// table: clock time for today, date for older, "-" when the item
// never started (or predates the started_at field).
func weaveStartedCol(it *weaveItem) string {
	if it.StartedAt.IsZero() {
		return "-"
	}
	t := it.StartedAt.Local()
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04:05")
	}
	return t.Format("Jan02")
}

// weaveDurationCol renders elapsed run time: live (now-started) for
// working items, started→finished for terminal ones, "-" otherwise.
func weaveDurationCol(it *weaveItem) string {
	if it.StartedAt.IsZero() {
		return "-"
	}
	var d time.Duration
	switch {
	case it.State == "working":
		d = time.Since(it.StartedAt)
	case !it.FinishedAt.IsZero():
		d = it.FinishedAt.Sub(it.StartedAt)
	default:
		return "-"
	}
	if d < 0 {
		return "-"
	}
	d = d.Round(time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func findWeaveItem(q *weaveQueue, id int64) *weaveItem {
	for _, it := range q.Items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func nextTodo(q *weaveQueue) *weaveItem {
	var todos []*weaveItem
	for _, it := range q.Items {
		if it.State == "todo" {
			todos = append(todos, it)
		}
	}
	sort.SliceStable(todos, func(i, j int) bool {
		pi := prioRank(todos[i].Priority)
		pj := prioRank(todos[j].Priority)
		if pi != pj {
			return pi < pj
		}
		return todos[i].ID < todos[j].ID
	})
	if len(todos) == 0 {
		return nil
	}
	return todos[0]
}

func prioRank(p string) int {
	switch p {
	case "p0":
		return 0
	case "p1":
		return 1
	case "p3":
		return 3
	default:
		return 2
	}
}

func ec(code int) error {
	if code == 0 {
		return nil
	}
	return &exitCodeError{code: code}
}

func runWeaveAdd(cmd *cobra.Command, title, body, priority string, flags *weaveOutputFlags) error {
	mode := flags.mode()
	if title == "" {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitInvalidArg, fmt.Errorf("title required")))
	}
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitPrecondFail, err))
	}
	dir, err := weaveQueueDir(root)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitGenericFail, err))
	}
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitGenericFail, err))
	}
	prio := priority
	if prio == "" {
		prio = "p2"
	}
	it := &weaveItem{
		ID:       q.NextID,
		Title:    title,
		Body:     body,
		Priority: prio,
		State:    "todo",
		Created:  time.Now().UTC(),
	}
	q.NextID++
	q.Items = append(q.Items, it)
	if err := saveWeaveQueue(dir, q); err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitGenericFail, err))
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave add", map[string]any{
			"issue":    it.ID,
			"title":    it.Title,
			"priority": it.Priority,
			"state":    it.State,
		}))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "weave add: issue #%d created (%s, todo) — %q\n", it.ID, it.Priority, it.Title)
	return nil
}

func runWeaveList(cmd *cobra.Command, includeHistory bool, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave list",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave list",
			weavecli.ExitGenericFail, err))
	}
	var items []*weaveItem
	anyStale := false
	for _, it := range q.Items {
		if !includeHistory && (it.State == "done" || it.State == "abandoned") {
			continue
		}
		// Computed, never persisted: a "working" item whose wrapper
		// died without reaching the terminal-state write (crash,
		// SIGKILL, machine OOM) would otherwise claim to be working
		// forever and silently block `weave wait --all`.
		if it.State == "working" && it.WrapperPid > 0 && !pidAlive(it.WrapperPid) {
			it.Stale = true
			anyStale = true
		}
		items = append(items, it)
	}
	var others []map[string]any
	if len(items) == 0 {
		others = weaveOtherActiveQueues(dir)
	}
	if mode == weavecli.OutputJSON {
		res := map[string]any{"items": items}
		if len(others) > 0 {
			res["other_active_queues"] = others
		}
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave list", res))
	}
	if len(items) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "weave list: queue empty for %s\n", root)
		if len(others) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "  hint: queues are per-repo (keyed by cwd); active weaves exist elsewhere:")
			for _, o := range others {
				fmt.Fprintf(cmd.OutOrStdout(), "    %s (%d active) — cd there and re-run\n", o["root"], o["active"])
			}
		}
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%-4s %-4s %-10s %-8s %-8s %-40s %s\n", "ID", "PRIO", "STATE", "STARTED", "DUR", "TITLE", "SANDBOX")
	for _, it := range items {
		title := it.Title
		if len(title) > 40 {
			title = title[:37] + "..."
		}
		state := it.State
		if it.Stale {
			state = it.State + "*"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-4d %-4s %-10s %-8s %-8s %-40s %s\n",
			it.ID, it.Priority, state, weaveStartedCol(it), weaveDurationCol(it), title, it.Sandbox)
	}
	if anyStale {
		fmt.Fprintln(cmd.OutOrStdout(), "* wrapper process is dead — re-attach with `weave start --resume --issue N` or `weave abandon N`")
	}
	return nil
}

func runWeaveNext(cmd *cobra.Command, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave next",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	q, _ := loadWeaveQueue(dir)
	it := nextTodo(q)
	if it == nil {
		if mode == weavecli.OutputJSON {
			return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave next", map[string]any{"empty": true}))
		}
		fmt.Fprintln(cmd.OutOrStdout(), "weave next: queue empty")
		return nil
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave next", map[string]any{
			"issue":    it.ID,
			"title":    it.Title,
			"priority": it.Priority,
		}))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "weave next: #%d (%s) %q\n", it.ID, it.Priority, it.Title)
	return nil
}

// weaveStartOptions controls runWeaveStart behavior. noSpawn does
// every step up to and including state mutation but skips the tool
// exec. resume reattaches to an existing "working" sandbox without
// rebuilding the worktree — useful when an agent crashed and the
// user wants to re-launch it inside the same sandbox without losing
// in-progress changes. pty controls TTY allocation for the subagent
// (auto = on, always = on, never = inherit FDs). idleTimeout, when
// > 0, sends SIGTERM to the subagent if no PTY output appears for
// that long — the dogfood found that some TUI agents (claude TUI,
// when launched without -p) never exit on their own and need a
// heuristic kill on idle.
type weaveStartOptions struct {
	noSpawn     bool
	resume      bool
	pty         string // "auto" (default), "always", "never"
	idleTimeout time.Duration
	maxRuntime  time.Duration
	memLimit    string // e.g. "16g"; "0" disables
}

// weaveGuards bundles the subagent watchdog limits threaded into
// runWeaveToolPTY. Three independent tripwires, each SIGTERMing the
// subagent's process tree:
//   - idleTimeout: no PTY output for this long (stuck-TUI heuristic;
//     useless against a runaway TUI, whose spinner keeps emitting).
//   - maxRuntime: hard wall-clock ceiling, immune to spinner output.
//   - memLimitBytes: total RSS of the subagent's process tree. This
//     is the OOM backstop — whatever the leak mechanism, the agent
//     dies at the budget instead of taking the machine down.
type weaveGuards struct {
	idleTimeout   time.Duration
	maxRuntime    time.Duration
	memLimitBytes int64
	// ctlSock, when non-empty, is the unix socket runWeaveToolPTY
	// serves for `weave say`: each line received is written to the
	// PTY master with a trailing \r — keystrokes, as far as the
	// subagent can tell.
	ctlSock string
}

// errWeaveWrapperLive is returned from inside the queue-lock callback
// when the issue already has a live wrapper process; runWeaveStart
// translates it into an ExitStateConflict envelope instead of the
// generic "queue write failed (continuing)" path.
var errWeaveWrapperLive = errors.New("wrapper already running")

// parseWeaveMemLimit parses a human byte size ("16g", "512m",
// "1024k", plain bytes). Empty or "0" disables the limit.
func parseWeaveMemLimit(s string) (int64, error) {
	orig := s
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "0" {
		return 0, nil
	}
	s = strings.TrimSuffix(s, "b")
	mult := int64(1)
	if len(s) > 0 {
		switch s[len(s)-1] {
		case 'k':
			mult, s = 1<<10, s[:len(s)-1]
		case 'm':
			mult, s = 1<<20, s[:len(s)-1]
		case 'g':
			mult, s = 1<<30, s[:len(s)-1]
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid --mem-limit %q (want e.g. 16g, 512m, 0 to disable)", orig)
	}
	return n * mult, nil
}

// weavePTYMode returns the normalized PTY mode for runWeaveStart.
func (o weaveStartOptions) ptyMode() string {
	switch o.pty {
	case "always", "never", "auto":
		return o.pty
	case "":
		return "auto"
	default:
		return "auto"
	}
}

func runWeaveStart(cmd *cobra.Command, issueID int64, toolFlag string, toolArgs []string, opts weaveStartOptions, flags *weaveOutputFlags) error {
	mode := flags.mode()
	if len(toolArgs) == 0 && toolFlag != "" {
		toolArgs = []string{toolFlag}
	}
	if !opts.noSpawn && len(toolArgs) == 0 {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
			weavecli.ExitInvalidArg, fmt.Errorf("provide trailing '-- <tool> [args...]' or --tool <name> (or pass --no-spawn to allocate only)")))
	}
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
			weavecli.ExitGenericFail, err))
	}
	var it *weaveItem
	if issueID > 0 {
		it = findWeaveItem(q, issueID)
		if it == nil {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
				weavecli.ExitInvalidArg, fmt.Errorf("issue #%d not found", issueID)))
		}
	} else {
		if opts.resume {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
				weavecli.ExitInvalidArg, fmt.Errorf("--resume requires --issue <id>")))
		}
		it = nextTodo(q)
		if it == nil {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
				weavecli.ExitPrecondFail, fmt.Errorf("queue empty")))
		}
	}
	if it.State == "done" || it.State == "abandoned" {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
			weavecli.ExitStateConflict, fmt.Errorf("issue #%d state is %q", it.ID, it.State)))
	}
	if opts.resume {
		// Any state with a preserved sandbox is resumable: "working"
		// (wrapper died), "failed" (weave kill / watchdog kill — the
		// retry path the kill docs promise), "submitted" (tool exited
		// but the branch was kicked back, e.g. merge conflict). done
		// and abandoned were rejected above; their sandboxes are gone.
		if it.Sandbox == "" {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
				weavecli.ExitStateConflict, fmt.Errorf("--resume: issue #%d has no sandbox to reattach (state=%q)", it.ID, it.State)))
		}
		if _, err := os.Stat(it.Sandbox); err != nil {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
				weavecli.ExitStateConflict, fmt.Errorf("--resume: sandbox missing on disk: %s", it.Sandbox)))
		}
	}
	base := weaveBaseBranch(root)
	sandbox := filepath.Join(dir, "sandboxes", fmt.Sprintf("issue-%d", it.ID))
	branch := fmt.Sprintf("agent/weave-issue-%d", it.ID)
	// Control socket for `weave say` — only meaningful when the
	// subagent gets a PTY and we're actually spawning it.
	ctlSock := ""
	if opts.ptyMode() != "never" && !opts.noSpawn {
		ctlSock = weaveCtlSockPath(dir, it.ID)
	}
	if opts.resume {
		sandbox = it.Sandbox
		branch = it.Branch
		// Re-claim the issue under the queue lock: refuse when a
		// previous wrapper is still alive (two wrappers in one
		// sandbox = two agents fighting over the same checkout —
		// the dogfood OOM had a stale wrapper_pid precisely
		// because resume skipped this bookkeeping), and record
		// OUR pid so `weave kill` / `weave abandon` signal the
		// process that is actually running, not a long-dead one.
		lockErr := withWeaveQueueLock(dir, func(freshQ *weaveQueue) error {
			freshIt := findWeaveItem(freshQ, it.ID)
			if freshIt == nil {
				return fmt.Errorf("queue lock: issue #%d disappeared", it.ID)
			}
			if freshIt.WrapperPid > 0 && freshIt.WrapperPid != os.Getpid() && pidAlive(freshIt.WrapperPid) {
				return fmt.Errorf("issue #%d already has a live wrapper (pid %d); run `ycode weave kill --issue %d` first: %w",
					it.ID, freshIt.WrapperPid, it.ID, errWeaveWrapperLive)
			}
			freshIt.WrapperPid = os.Getpid()
			// Flip back to working and clear the stale terminal
			// record — otherwise `weave list` shows failed while an
			// agent is actively running and `weave wait` returns
			// immediately on the old terminal state.
			freshIt.State = "working"
			freshIt.ExitCode = nil
			freshIt.StartedAt = time.Now().UTC()
			freshIt.FinishedAt = time.Time{}
			freshIt.CtlSock = ctlSock
			it = freshIt
			return nil
		})
		if lockErr != nil {
			code := weavecli.ExitGenericFail
			if errors.Is(lockErr, errWeaveWrapperLive) {
				code = weavecli.ExitStateConflict
			}
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start", code, lockErr))
		}
	}
	if !opts.resume {
		if _, err := os.Stat(sandbox); err != nil {
			if err := os.MkdirAll(filepath.Dir(sandbox), 0o755); err != nil {
				return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
					weavecli.ExitGenericFail, err))
			}
			// Sandbox isolation: a full local clone, NOT a worktree.
			// git worktree shares `.git/objects` and `.git/refs` with
			// the source repo — an agent that wandered out of its
			// sandbox cwd (cd to the source checkout, or `git
			// update-ref`) could mutate the source's branches. With a
			// clone, the sandbox has its own `.git`; refs and HEAD
			// can't cross the boundary, and a wandering agent hits a
			// different git repo entirely.
			gw := exec.Command("git", "clone", "--local", "--branch", base, root, sandbox)
			gw.Stdout = cmd.OutOrStdout()
			gw.Stderr = cmd.ErrOrStderr()
			if err := gw.Run(); err != nil {
				return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
					weavecli.ExitGenericFail, fmt.Errorf("git clone --local: %w", err)))
			}
			// Check out the per-issue agent branch in the clone.
			ck := exec.Command("git", "-C", sandbox, "checkout", "-b", branch)
			ck.Stdout = cmd.OutOrStdout()
			ck.Stderr = cmd.ErrOrStderr()
			if err := ck.Run(); err != nil {
				return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
					weavecli.ExitGenericFail, fmt.Errorf("git checkout -b %s: %w", branch, err)))
			}
			// Remove `origin` from the clone: it points at the user's
			// real checkout, and in dogfooding a subagent followed it
			// (`git remote -v`) to escape the sandbox and commit to the
			// origin repo's master directly. Nothing in the weave flow
			// needs the remote — `weave pull` fetches FROM the sandbox
			// path into the user's repo, never the other way around.
			_ = exec.Command("git", "-C", sandbox, "remote", "remove", "origin").Run()
			// Scrub reflogs: `git clone` records "clone: from <abs
			// origin path>" in .git/logs/HEAD — the breadcrumb the
			// second sandbox escape had available after the remote
			// was gone. git recreates reflogs as the agent works;
			// only the clone-time entries carry the origin path.
			_ = os.RemoveAll(filepath.Join(sandbox, ".git", "logs"))
		}
		for _, kv := range [][2]string{
			{"user.name", fmt.Sprintf("agent-weave-issue-%d", it.ID)},
			{"user.email", fmt.Sprintf("agent-weave-issue-%d@ycode.local", it.ID)},
		} {
			_ = exec.Command("git", "-C", sandbox, "config", kv[0], kv[1]).Run()
		}
		// Lock around the state=working transition so concurrent
		// `weave start --issue N` invocations targeting different
		// issues don't race on the queue.json write (last-write-
		// wins would silently strand one of the items).
		lockErr := withWeaveQueueLock(dir, func(freshQ *weaveQueue) error {
			freshIt := findWeaveItem(freshQ, it.ID)
			if freshIt == nil {
				return fmt.Errorf("queue lock: issue #%d disappeared", it.ID)
			}
			if freshIt.WrapperPid > 0 && freshIt.WrapperPid != os.Getpid() && pidAlive(freshIt.WrapperPid) {
				return fmt.Errorf("issue #%d already has a live wrapper (pid %d); run `ycode weave kill --issue %d` first: %w",
					it.ID, freshIt.WrapperPid, it.ID, errWeaveWrapperLive)
			}
			freshIt.State = "working"
			freshIt.Sandbox = sandbox
			freshIt.Branch = branch
			freshIt.WrapperPid = os.Getpid()
			freshIt.CtlSock = ctlSock
			freshIt.StartedAt = time.Now().UTC()
			it = freshIt
			return nil
		})
		if lockErr != nil {
			if errors.Is(lockErr, errWeaveWrapperLive) {
				return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
					weavecli.ExitStateConflict, lockErr))
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "weave start: queue write failed (continuing): %v\n", lockErr)
		}
	}
	if mode != weavecli.OutputJSON {
		fmt.Fprintf(cmd.OutOrStdout(), "weave start: issue #%d sandbox=%s branch=%s\n", it.ID, sandbox, branch)
		if opts.noSpawn {
			fmt.Fprintf(cmd.OutOrStdout(), "weave start: --no-spawn (skipping tool exec)\n")
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "weave start: launching %s ...\n", strings.Join(toolArgs, " "))
		}
	}
	if opts.noSpawn {
		if mode == weavecli.OutputJSON {
			return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave start", map[string]any{
				"issue":    it.ID,
				"sandbox":  sandbox,
				"branch":   branch,
				"state":    "working",
				"no_spawn": true,
			}))
		}
		return nil
	}
	// Containment: the subagent must not learn the origin repo's
	// path from its environment. The orchestrator's shell typically
	// sits in the origin repo, so the inherited PWD/OLDPWD point
	// straight at it — scrub them and pin PWD to the sandbox.
	env := make([]string, 0, len(os.Environ())+8)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PWD=") || strings.HasPrefix(kv, "OLDPWD=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "PWD="+sandbox)
	env = append(env,
		fmt.Sprintf("YCODE_LOOM_ID=weave-issue-%d", it.ID),
		fmt.Sprintf("YCODE_LOOM_BRANCH=%s", branch),
		fmt.Sprintf("YCODE_LOOM_BASE=%s", base),
		fmt.Sprintf("YCODE_LOOM_ISSUE=%d", it.ID),
		fmt.Sprintf("YCODE_LOOM_ISSUE_TITLE=%s", it.Title),
		fmt.Sprintf("YCODE_LOOM_ISSUE_BODY=%s", it.Body),
	)
	tool := exec.Command(toolArgs[0], toolArgs[1:]...)
	tool.Dir = sandbox
	tool.Env = env

	// PTY allocation policy:
	//   - never:        inherit FDs (legacy, breaks TUI subagents).
	//   - always|auto:  allocate a PTY. When parent stdin is a TTY,
	//                   pass-through interactively (raw mode). When
	//                   parent stdin is NOT a TTY (orchestrator pipe
	//                   or backgrounded by shell &), route subagent
	//                   PTY output to a per-issue log file under the
	//                   queue dir so the subagent renders correctly
	//                   AND we don't pump its TUI output back into
	//                   the orchestrator's pipe (the OOM footgun the
	//                   original incident exposed).
	memLimitBytes, err := parseWeaveMemLimit(opts.memLimit)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
			weavecli.ExitInvalidArg, err))
	}
	guards := weaveGuards{
		idleTimeout:   opts.idleTimeout,
		maxRuntime:    opts.maxRuntime,
		memLimitBytes: memLimitBytes,
		ctlSock:       ctlSock,
	}
	if ctlSock != "" {
		if err := os.MkdirAll(filepath.Dir(ctlSock), 0o755); err != nil {
			// Non-fatal: `weave say` degrades to state_conflict.
			guards.ctlSock = ""
		}
	}
	ptyMode := opts.ptyMode()
	parentStdinTTY := weaveStdinIsTTY()
	useLogFile := ptyMode != "never" && !parentStdinTTY
	var logFile *os.File
	var logPath string
	if useLogFile {
		logsDir := filepath.Join(dir, "logs")
		if err := os.MkdirAll(logsDir, 0o755); err != nil {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
				weavecli.ExitGenericFail, fmt.Errorf("create log dir: %w", err)))
		}
		logPath = filepath.Join(logsDir, fmt.Sprintf("issue-%d.log", it.ID))
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
				weavecli.ExitGenericFail, fmt.Errorf("open log: %w", err)))
		}
		logFile = f
	}
	if mode != weavecli.OutputJSON && useLogFile {
		fmt.Fprintf(cmd.OutOrStdout(), "weave start: PTY → %s\n", logPath)
	}

	// Auto-detach from the parent shell's session when invoked
	// non-interactively. Without this, a backgrounded ycode (e.g.
	// `ycode weave start ... &` from a script) receives SIGHUP when
	// the launching shell exits, killing the subagent partway
	// through its work. Setsid puts us in a new session so we
	// outlive the launcher. Only safe when stdin is non-TTY — a
	// user at a terminal expects to be able to ^C their own
	// invocation, which Setsid would break.
	weaveMaybeSetsid(parentStdinTTY)

	var (
		exitCode int
		runErr   error
	)
	if ptyMode == "never" {
		tool.Stdin = os.Stdin
		tool.Stdout = os.Stdout
		tool.Stderr = os.Stderr
		runErr = tool.Run()
		if runErr != nil {
			if ee, ok := runErr.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
				runErr = nil
			} else {
				exitCode = 1
			}
		}
	} else {
		if useLogFile {
			// Subagent stdio: PTY ↔ log file. stdin is the PTY slave
			// (no user input source); stdout/stderr go to the PTY
			// master which we copy to logFile.
			exitCode, runErr = runWeaveToolPTY(tool, logFile, guards)
			_ = logFile.Close()
		} else {
			// Interactive TTY pass-through.
			exitCode, runErr = runWeaveToolPTY(tool, nil, guards)
		}
	}

	// Persist the outcome regardless of envelope mode — `weave wait`
	// and `weave pull` read the queue, not stdout. Take the queue
	// lock for the final read-modify-write so concurrent
	// `weave start` calls (the orchestrator's parallel-agent
	// pattern) don't clobber each other's terminal-state updates.
	// We re-load inside the lock to pick up any updates that
	// landed while the tool was running.
	finishedAt := time.Now().UTC()
	lockErr := withWeaveQueueLock(dir, func(freshQ *weaveQueue) error {
		freshIt := findWeaveItem(freshQ, it.ID)
		if freshIt == nil {
			return fmt.Errorf("queue lock: issue #%d disappeared", it.ID)
		}
		freshIt.FinishedAt = finishedAt
		freshIt.ExitCode = &exitCode
		freshIt.WrapperPid = 0
		freshIt.CtlSock = ""
		if logPath != "" {
			freshIt.LogPath = logPath
		}
		if exitCode == 0 && runErr == nil {
			freshIt.State = "submitted"
		} else {
			freshIt.State = "failed"
		}
		it = freshIt
		return nil
	})
	if lockErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "weave start: queue write failed after tool exit: %v\n", lockErr)
	}

	if runErr != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
			weavecli.ExitGenericFail, runErr))
	}
	if exitCode != 0 {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
			weavecli.ExitGenericFail, fmt.Errorf("tool exited with %d", exitCode)))
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave start", map[string]any{
			"issue":     it.ID,
			"sandbox":   sandbox,
			"branch":    branch,
			"state":     it.State,
			"exit_code": exitCode,
			"log_path":  logPath,
		}))
	}
	return nil
}

// runWeaveSay injects one line into a running subagent's PTY via
// the wrapper's per-issue control socket. The wrapper appends \r,
// so the TUI treats it as a submitted message.
func runWeaveSay(cmd *cobra.Command, id int64, text string, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave say",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave say",
			weavecli.ExitGenericFail, err))
	}
	it := findWeaveItem(q, id)
	if it == nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave say",
			weavecli.ExitInvalidArg, fmt.Errorf("issue #%d not found", id)))
	}
	if it.State != "working" || it.WrapperPid == 0 || !pidAlive(it.WrapperPid) {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave say",
			weavecli.ExitStateConflict, fmt.Errorf("issue #%d has no live subagent (state=%q)", it.ID, it.State)))
	}
	if it.CtlSock == "" {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave say",
			weavecli.ExitStateConflict, fmt.Errorf("issue #%d has no control socket — its wrapper predates `weave say` or ran with --pty=never", it.ID)))
	}
	conn, err := net.DialTimeout("unix", it.CtlSock, 3*time.Second)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave say",
			weavecli.ExitDepUnhealthy, fmt.Errorf("control socket dial: %w", err)))
	}
	defer conn.Close()
	// Strip newlines so one say = one injected line; the wrapper
	// adds the terminating \r itself.
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r", " "), "\n", " ")
	if _, err := fmt.Fprintln(conn, text); err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave say",
			weavecli.ExitGenericFail, fmt.Errorf("control socket write: %w", err)))
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave say", map[string]any{
			"issue": it.ID,
			"sent":  text,
		}))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "weave say: sent to issue #%d — watch `weave log %d -f`\n", it.ID, it.ID)
	return nil
}

// runWeaveLog prints the captured PTY log for an issue, optionally
// following appended output until the issue reaches a terminal
// state. The capture file only exists when the subagent ran with a
// captured PTY (non-TTY parent); interactive passthrough sessions
// have nothing recorded.
func runWeaveLog(cmd *cobra.Command, id int64, follow bool, tailN int, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave log",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave log",
			weavecli.ExitGenericFail, err))
	}
	it := findWeaveItem(q, id)
	if it == nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave log",
			weavecli.ExitInvalidArg, fmt.Errorf("issue #%d not found", id)))
	}
	logPath := it.LogPath
	if logPath == "" {
		// The queue persists log_path on exit; while the subagent is
		// still running, fall back to the conventional capture path —
		// watching a LIVE issue is this subverb's main use case.
		conventional := filepath.Join(dir, "logs", fmt.Sprintf("issue-%d.log", it.ID))
		if _, err := os.Stat(conventional); err == nil {
			logPath = conventional
		}
	}
	if logPath == "" {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave log",
			weavecli.ExitStateConflict, fmt.Errorf("issue #%d has no PTY capture (state=%q) — it either hasn't started or ran interactively (PTY passthrough)", it.ID, it.State)))
	}
	f, err := os.Open(logPath)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave log",
			weavecli.ExitStateConflict, fmt.Errorf("log missing on disk: %s", logPath)))
	}
	defer f.Close()
	if mode == weavecli.OutputJSON {
		// Agent mode: the raw PTY stream isn't envelope-safe; return
		// the metadata and let the caller read/tail the file itself.
		st, statErr := f.Stat()
		var size int64
		if statErr == nil {
			size = st.Size()
		}
		res := map[string]any{
			"issue":      it.ID,
			"state":      it.State,
			"log_path":   logPath,
			"size_bytes": size,
		}
		if it.ExitCode != nil {
			res["exit_code"] = *it.ExitCode
		}
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave log", res))
	}
	if tailN >= 0 {
		off, err := tailOffset(f, tailN)
		if err != nil {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave log",
				weavecli.ExitGenericFail, err))
		}
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave log",
				weavecli.ExitGenericFail, err))
		}
	}
	out := cmd.OutOrStdout()
	if _, err := io.Copy(out, f); err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave log",
			weavecli.ExitGenericFail, err))
	}
	if !follow {
		return nil
	}
	// Follow: poll for appended bytes (regular files don't support
	// blocking reads past EOF). Stop once the issue is terminal AND
	// the file is drained — terminal-then-drain, not drain-then-
	// terminal, so the final flush after exit is never truncated.
	for {
		time.Sleep(500 * time.Millisecond)
		n, err := io.Copy(out, f)
		if err != nil {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave log",
				weavecli.ExitGenericFail, err))
		}
		if n > 0 {
			continue
		}
		q2, err := loadWeaveQueue(dir)
		if err != nil {
			continue // transient queue read race; keep following
		}
		it2 := findWeaveItem(q2, id)
		if it2 == nil || isTerminalState(it2.State) {
			return nil
		}
	}
}

// tailOffset returns the byte offset where the last n lines of f
// begin, with tail(1) semantics: a trailing newline terminates the
// final line rather than starting an empty one. n<=0 returns the
// end offset (print nothing; with -f that means "new output only").
func tailOffset(f *os.File, n int) (int64, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()
	if n <= 0 || size == 0 {
		return size, nil
	}
	end := size
	one := make([]byte, 1)
	if _, err := f.ReadAt(one, size-1); err == nil && one[0] == '\n' {
		end = size - 1
	}
	const chunk = 32 * 1024
	buf := make([]byte, chunk)
	count := 0
	pos := end
	for pos > 0 {
		readSize := int64(chunk)
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		m, err := f.ReadAt(buf[:readSize], pos)
		if err != nil && m <= 0 {
			return 0, err
		}
		for i := m - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				count++
				if count == n {
					return pos + int64(i) + 1, nil
				}
			}
		}
	}
	return 0, nil
}

func runWeavePull(cmd *cobra.Command, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave pull",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave pull",
			weavecli.ExitGenericFail, err))
	}
	type result struct {
		Issue  int64  `json:"issue"`
		Branch string `json:"branch"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
	}
	var results []result
	dirty := false
	for _, it := range q.Items {
		// Merge any branch belonging to an item that's either still
		// running (working — predates state transitions) or that
		// finished cleanly (submitted). Items in "failed", "done",
		// or "abandoned" are skipped: failed shouldn't auto-merge,
		// done is already merged, abandoned was torn down.
		if it.State != "working" && it.State != "submitted" {
			continue
		}
		// The agent's branch lives in the sandbox clone, not the
		// user's repo. Fetch it across (idempotent — already-present
		// commits are skipped). If the sandbox is gone (abandoned
		// mid-pull, disk wiped) we record a skip with the reason.
		if it.Sandbox == "" {
			results = append(results, result{Issue: it.ID, Branch: it.Branch, Status: "skipped", Detail: "no sandbox recorded"})
			continue
		}
		if _, err := os.Stat(it.Sandbox); err != nil {
			results = append(results, result{Issue: it.ID, Branch: it.Branch, Status: "skipped", Detail: fmt.Sprintf("sandbox missing: %v", err)})
			continue
		}
		fetchSpec := fmt.Sprintf("%s:%s", it.Branch, it.Branch)
		if _, err := gitOut(root, "fetch", "--no-tags", it.Sandbox, fetchSpec); err != nil {
			results = append(results, result{Issue: it.ID, Branch: it.Branch, Status: "skipped", Detail: fmt.Sprintf("fetch from sandbox: %v", err)})
			continue
		}
		cnt, err := gitOut(root, "rev-list", "--count", fmt.Sprintf("HEAD..%s", it.Branch))
		if err != nil {
			results = append(results, result{Issue: it.ID, Branch: it.Branch, Status: "skipped", Detail: err.Error()})
			continue
		}
		ahead, _ := strconv.Atoi(strings.TrimSpace(cnt))
		if ahead == 0 {
			results = append(results, result{Issue: it.ID, Branch: it.Branch, Status: "empty"})
			continue
		}
		mergeMsg := fmt.Sprintf("weave: merge issue #%d — %s", it.ID, it.Title)
		mc := exec.Command("git", "-C", root, "merge", "--no-ff", "-m", mergeMsg, it.Branch)
		out, err := mc.CombinedOutput()
		if err != nil {
			_ = exec.Command("git", "-C", root, "merge", "--abort").Run()
			results = append(results, result{Issue: it.ID, Branch: it.Branch, Status: "conflict", Detail: strings.TrimSpace(string(out))})
			continue
		}
		if it.Sandbox != "" {
			_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", it.Sandbox).Run()
		}
		_ = exec.Command("git", "-C", root, "branch", "-D", it.Branch).Run()
		it.State = "done"
		it.Sandbox = ""
		results = append(results, result{Issue: it.ID, Branch: it.Branch, Status: "merged"})
		dirty = true
	}
	if dirty {
		if err := saveWeaveQueue(dir, q); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "weave pull: queue save failed: %v\n", err)
		}
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave pull", map[string]any{
			"results": results,
		}))
	}
	if len(results) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "weave pull: nothing to merge")
		return nil
	}
	for _, r := range results {
		detail := ""
		if r.Detail != "" {
			detail = " — " + r.Detail
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  issue #%d (%s): %s%s\n", r.Issue, r.Branch, r.Status, detail)
	}
	return nil
}

func runWeaveAbandon(cmd *cobra.Command, id int64, reason string, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave abandon",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave abandon",
			weavecli.ExitGenericFail, err))
	}
	it := findWeaveItem(q, id)
	if it == nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave abandon",
			weavecli.ExitInvalidArg, fmt.Errorf("issue #%d not found", id)))
	}
	// If a wrapper PID is recorded and the item is still working,
	// signal precisely — SIGTERM the recorded PID, wait briefly,
	// escalate to SIGKILL. The wrapper is its own session leader
	// (auto-setsid on non-TTY), so SIGTERM reaches the subagent's
	// process group cleanly. This is the supported way to stop a
	// running weave; the dogfood found that `pkill -f` would also
	// catch peer ycode/claude sessions belonging to other agents,
	// which is dangerous in a shared agentic environment.
	if it.State == "working" && it.WrapperPid > 0 {
		weaveStopWrapper(it.WrapperPid)
	}
	// Sandbox is a real git clone now (not a worktree); delete the
	// directory tree. The agent's branch lives inside that clone —
	// no separate `git branch -D` against the user's repo because
	// the branch doesn't exist there unless `weave pull` fetched it.
	if it.Sandbox != "" {
		_ = os.RemoveAll(it.Sandbox)
	}
	if it.Branch != "" {
		// Best-effort: drop the branch from the user's repo too, in
		// case `weave pull` fetched it earlier.
		_ = exec.Command("git", "-C", root, "branch", "-D", it.Branch).Run()
	}
	it.State = "abandoned"
	it.Sandbox = ""
	it.WrapperPid = 0
	if err := saveWeaveQueue(dir, q); err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave abandon",
			weavecli.ExitGenericFail, err))
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave abandon", map[string]any{
			"issue":  it.ID,
			"state":  it.State,
			"reason": reason,
		}))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "weave abandon: issue #%d abandoned\n", it.ID)
	return nil
}

func gitOut(root string, args ...string) (string, error) {
	a := append([]string{"-C", root}, args...)
	out, err := exec.Command("git", a...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// runWeavePrio sets an issue's priority tier on the local queue.
// --auto (LLM-rank the whole queue) requires an LLM provider and is
// not available in the local backend; we emit a precondition_failed
// envelope so agent callers see a stable shape.
func runWeavePrio(cmd *cobra.Command, id int64, tier string, auto bool, flags *weaveOutputFlags) error {
	mode := flags.mode()
	if auto {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave prio",
			weavecli.ExitDepUnhealthy, fmt.Errorf("--auto requires an LLM provider; not available in the local backend (run `ycode serve` for the forge backend)")))
	}
	if !isValidPriority(tier) {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave prio",
			weavecli.ExitInvalidArg, fmt.Errorf("priority must be one of p0|p1|p2|p3 (got %q)", tier)))
	}
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave prio",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave prio",
			weavecli.ExitGenericFail, err))
	}
	it := findWeaveItem(q, id)
	if it == nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave prio",
			weavecli.ExitInvalidArg, fmt.Errorf("issue #%d not found", id)))
	}
	prev := it.Priority
	it.Priority = tier
	if err := saveWeaveQueue(dir, q); err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave prio",
			weavecli.ExitGenericFail, err))
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave prio", map[string]any{
			"issue":    it.ID,
			"priority": it.Priority,
			"previous": prev,
			"title":    it.Title,
		}))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "weave prio: issue #%d %s → %s\n", it.ID, prev, it.Priority)
	return nil
}

// runWeaveShell drops the user into $SHELL with cwd set to the
// issue's sandbox so they can poke at the worktree directly.
// Inherits stdio; exits with the shell's exit code.
func runWeaveShell(cmd *cobra.Command, id int64, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave shell",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave shell",
			weavecli.ExitGenericFail, err))
	}
	it := findWeaveItem(q, id)
	if it == nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave shell",
			weavecli.ExitInvalidArg, fmt.Errorf("issue #%d not found", id)))
	}
	if it.Sandbox == "" {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave shell",
			weavecli.ExitStateConflict, fmt.Errorf("issue #%d has no sandbox (state=%q) — run `weave start --issue %d --no-spawn` first", it.ID, it.State, it.ID)))
	}
	if _, err := os.Stat(it.Sandbox); err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave shell",
			weavecli.ExitStateConflict, fmt.Errorf("sandbox missing on disk: %s", it.Sandbox)))
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	if mode == weavecli.OutputJSON {
		// Agent mode: return the sandbox + shell info instead of execing
		// — agents can't drive an interactive shell anyway.
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave shell", map[string]any{
			"issue":   it.ID,
			"sandbox": it.Sandbox,
			"branch":  it.Branch,
			"shell":   shell,
		}))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "weave shell: issue #%d sandbox=%s (exit shell to return)\n", it.ID, it.Sandbox)
	sh := exec.Command(shell)
	sh.Dir = it.Sandbox
	sh.Env = append(os.Environ(),
		fmt.Sprintf("YCODE_LOOM_ID=weave-issue-%d", it.ID),
		fmt.Sprintf("YCODE_LOOM_ISSUE=%d", it.ID),
		fmt.Sprintf("YCODE_LOOM_BRANCH=%s", it.Branch),
		fmt.Sprintf("YCODE_LOOM_ISSUE_TITLE=%s", it.Title),
	)
	sh.Stdin = os.Stdin
	sh.Stdout = os.Stdout
	sh.Stderr = os.Stderr
	if err := sh.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return &exitCodeError{code: exit.ExitCode()}
		}
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave shell",
			weavecli.ExitGenericFail, err))
	}
	return nil
}

// runWeaveReset tears down every weave for the current repo:
// removes each worktree, deletes each branch, and clears the queue
// file. Refuses without --yes unless stdin is a TTY and the user
// confirms at the prompt.
func runWeaveReset(cmd *cobra.Command, yes bool, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave reset",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave reset",
			weavecli.ExitGenericFail, err))
	}
	if !yes {
		// Agent mode (or any non-TTY) without --yes is a refusal — a
		// destructive op shouldn't run without explicit confirmation.
		if mode == weavecli.OutputJSON || !stdinIsTTY() {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave reset",
				weavecli.ExitInvalidArg, fmt.Errorf("refusing destructive reset without --yes")))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "weave reset: this removes every worktree + branch for this repo and clears the queue.\nproceed? [y/N] ")
		var resp string
		_, _ = fmt.Fscanln(os.Stdin, &resp)
		if !strings.EqualFold(resp, "y") && !strings.EqualFold(resp, "yes") {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave reset",
				weavecli.ExitInvalidArg, fmt.Errorf("cancelled")))
		}
	}
	type tear struct {
		Issue   int64  `json:"issue"`
		Branch  string `json:"branch,omitempty"`
		Sandbox string `json:"sandbox,omitempty"`
	}
	var teardowns []tear
	for _, it := range q.Items {
		if it.Sandbox == "" && it.Branch == "" && it.WrapperPid == 0 {
			continue
		}
		// Stop any still-running wrapper precisely (PID + setsid
		// group). Reset is a destructive batch op — we want
		// everything torn down cleanly.
		if it.WrapperPid > 0 {
			weaveStopWrapper(it.WrapperPid)
		}
		// Sandboxes are independent git clones — rm -rf is right.
		if it.Sandbox != "" {
			_ = os.RemoveAll(it.Sandbox)
		}
		if it.Branch != "" {
			// Best-effort: drop the branch from the user's repo if
			// `weave pull` fetched it earlier.
			_ = exec.Command("git", "-C", root, "branch", "-D", it.Branch).Run()
		}
		teardowns = append(teardowns, tear{Issue: it.ID, Branch: it.Branch, Sandbox: it.Sandbox})
	}
	// Remove the queue file itself; on next add it gets recreated.
	queuePath := filepath.Join(dir, "queue.json")
	_ = os.Remove(queuePath)
	// Best-effort: also remove the sandboxes/ tree in case the
	// individual removals left empty dirs behind.
	_ = os.RemoveAll(filepath.Join(dir, "sandboxes"))
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave reset", map[string]any{
			"teardowns": teardowns,
			"count":     len(teardowns),
		}))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "weave reset: tore down %d weaves; queue cleared\n", len(teardowns))
	return nil
}

// runWeaveOpen opens a Gitea page in the browser. Requires a running
// Gitea backend (`ycode serve`); in the local-only backend we emit a
// precondition_failed envelope explaining the dependency. The
// --issue N variant ALSO surfaces a file:// URL to the sandbox dir
// as a useful local-only fallback.
func runWeaveOpen(cmd *cobra.Command, issuesFlag, boardFlag, prFlag bool, issueID int64, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave open",
			weavecli.ExitPrecondFail, err))
	}
	// Local-fallback only: if --issue N points at a live sandbox, surface
	// its file:// URL even though the Gitea page itself isn't available.
	if issueID > 0 && !prFlag {
		dir, _ := weaveQueueDir(root)
		q, _ := loadWeaveQueue(dir)
		if it := findWeaveItem(q, issueID); it != nil && it.Sandbox != "" {
			fileURL := "file://" + it.Sandbox
			if mode == weavecli.OutputJSON {
				return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave open", map[string]any{
					"issue":       it.ID,
					"sandbox_url": fileURL,
					"forge_url":   nil,
					"backend":     "local",
					"note":        "forge-backed pages require `ycode serve`; surfacing sandbox path only.",
				}))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "weave open: issue #%d sandbox %s\n", it.ID, fileURL)
			return nil
		}
	}
	_, _, _ = issuesFlag, boardFlag, prFlag
	return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave open",
		weavecli.ExitDepUnhealthy, fmt.Errorf("requires the forge backend (run `ycode serve` first); the local backend has no web pages to open")))
}

// runWeaveInitBoard would create a Gitea kanban project board, but
// the local backend has no Gitea instance. Emit a clean dependency-
// unhealthy envelope so callers know to start `ycode serve`.
func runWeaveInitBoard(cmd *cobra.Command, flags *weaveOutputFlags) error {
	mode := flags.mode()
	return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave init-board",
		weavecli.ExitDepUnhealthy, fmt.Errorf("requires the forge backend (run `ycode serve` first); the local backend has no board to initialize")))
}

// addFromFile parses a markdown checklist or a JSON list and bulk-
// adds each entry to the queue. Returns the IDs created.
//
// Markdown shape (each line, ignoring leading/trailing whitespace):
//
//   - [ ] title goes here
//   - [ ] another title
//
// JSON shape: an array of objects with at minimum a `title` field;
// optional `body`, `priority`, `tool` overrides:
//
//	[
//	  {"title": "fix null deref", "priority": "p0"},
//	  {"title": "refactor user service", "body": "as discussed"}
//	]
func addFromFile(path string, defaultPriority string) ([]*weaveItem, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --from-file: %w", err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		// JSON array
		var entries []struct {
			Title    string `json:"title"`
			Body     string `json:"body"`
			Priority string `json:"priority"`
		}
		if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
			return nil, fmt.Errorf("parse --from-file as JSON: %w", err)
		}
		var out []*weaveItem
		for i, e := range entries {
			if strings.TrimSpace(e.Title) == "" {
				return nil, fmt.Errorf("--from-file entry %d: title required", i)
			}
			prio := e.Priority
			if prio == "" {
				prio = defaultPriority
			}
			out = append(out, &weaveItem{Title: e.Title, Body: e.Body, Priority: prio})
		}
		return out, nil
	}
	// Markdown checklist
	var out []*weaveItem
	for _, line := range strings.Split(string(raw), "\n") {
		l := strings.TrimSpace(line)
		// Match `- [ ] ...` or `- [x] ...` (case-insensitive).
		if len(l) < 6 || l[0] != '-' {
			continue
		}
		rest := strings.TrimSpace(l[1:])
		if len(rest) < 4 || rest[0] != '[' || rest[2] != ']' {
			continue
		}
		title := strings.TrimSpace(rest[3:])
		if title == "" {
			continue
		}
		out = append(out, &weaveItem{Title: title, Priority: defaultPriority})
	}
	return out, nil
}

// runWeaveAddFromFile bulk-adds from a markdown or JSON file. Each
// successful add increments NextID and emits one envelope (in JSON
// mode, a single result containing all added IDs).
func runWeaveAddFromFile(cmd *cobra.Command, path, defaultPriority string, flags *weaveOutputFlags) error {
	mode := flags.mode()
	if defaultPriority == "" {
		defaultPriority = "p2"
	}
	if !isValidPriority(defaultPriority) {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitInvalidArg, fmt.Errorf("priority must be one of p0|p1|p2|p3 (got %q)", defaultPriority)))
	}
	entries, err := addFromFile(path, defaultPriority)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitInvalidArg, err))
	}
	if len(entries) == 0 {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitInvalidArg, fmt.Errorf("--from-file %s contained no actionable entries", path)))
	}
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitPrecondFail, err))
	}
	dir, err := weaveQueueDir(root)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitGenericFail, err))
	}
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitGenericFail, err))
	}
	now := time.Now().UTC()
	type added struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		Priority string `json:"priority"`
	}
	var addedAll []added
	for _, e := range entries {
		e.ID = q.NextID
		q.NextID++
		e.State = "todo"
		e.Created = now
		q.Items = append(q.Items, e)
		addedAll = append(addedAll, added{ID: e.ID, Title: e.Title, Priority: e.Priority})
	}
	if err := saveWeaveQueue(dir, q); err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave add",
			weavecli.ExitGenericFail, err))
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave add", map[string]any{
			"added":  addedAll,
			"count":  len(addedAll),
			"source": path,
		}))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "weave add: bulk-added %d issues from %s\n", len(addedAll), path)
	return nil
}

// runWeaveListWatch polls the queue file every interval and emits
// a transition event (NDJSON envelope in JSON mode, one line in
// human modes) every time an item's state changes. Terminates on
// SIGINT or when the command context is cancelled.
func runWeaveListWatch(cmd *cobra.Command, includeHistory bool, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave list",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	prev := map[int64]string{}
	snapshot := func() (map[int64]string, []*weaveItem, error) {
		q, err := loadWeaveQueue(dir)
		if err != nil {
			return nil, nil, err
		}
		cur := map[int64]string{}
		var items []*weaveItem
		for _, it := range q.Items {
			if !includeHistory && (it.State == "done" || it.State == "abandoned") {
				continue
			}
			cur[it.ID] = it.State
			items = append(items, it)
		}
		return cur, items, nil
	}
	// Initial snapshot — emit as a synthetic "snapshot" event so a
	// watcher gets the full picture at t=0.
	cur, items, err := snapshot()
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave list",
			weavecli.ExitGenericFail, err))
	}
	emitInitial := func() {
		if mode == weavecli.OutputJSON {
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"schema_version": weavecli.SchemaVersion,
				"command":        "weave list",
				"status":         "ok",
				"event":          "snapshot",
				"items":          items,
			})
			return
		}
		fmt.Fprintf(cmd.OutOrStdout(), "weave list --watch: %d active issue(s) at t=0\n", len(items))
		for _, it := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "  #%d %-10s %s — %s\n", it.ID, it.State, it.Priority, it.Title)
		}
	}
	emitInitial()
	prev = cur

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		cur, _, err := snapshot()
		if err != nil {
			// Don't kill the watch on transient read errors — queue.json
			// is rewritten via tmp+rename, so a read between writes can
			// fail with ENOENT briefly. Skip this tick.
			continue
		}
		for id, st := range cur {
			if prev[id] != st {
				if mode == weavecli.OutputJSON {
					_ = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
						"schema_version": weavecli.SchemaVersion,
						"command":        "weave list",
						"status":         "ok",
						"event":          "transition",
						"issue":          id,
						"from":           prev[id],
						"to":             st,
					})
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "  #%d %s → %s\n", id, prev[id], st)
				}
			}
		}
		for id, st := range prev {
			if _, ok := cur[id]; !ok {
				if mode == weavecli.OutputJSON {
					_ = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
						"schema_version": weavecli.SchemaVersion,
						"command":        "weave list",
						"status":         "ok",
						"event":          "removed",
						"issue":          id,
						"from":           st,
					})
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "  #%d %s → (removed)\n", id, st)
				}
			}
		}
		prev = cur
	}
}

// isValidPriority returns true for any of the accepted priority tiers.
func isValidPriority(s string) bool {
	switch s {
	case "p0", "p1", "p2", "p3":
		return true
	}
	return false
}

// stdinIsTTY reports whether stdin is a terminal. Used by reset to
// distinguish "user at a terminal who can answer a prompt" from
// "scripted invocation that needs --yes".
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// runWeaveKill stops the running wrapper for an issue WITHOUT
// tearing down its sandbox or branch. Use when a subagent has gone
// stuck (no output for a long time, runaway iteration, etc.) and
// the orchestrator wants the partial work preserved for inspection
// or for a `weave start --resume` retry.
//
// The orchestrator-safe shape: orchestrators MUST NOT shell out to
// pkill / killall / kill -9 — those match by name and will catch
// peer ycode/claude/codex sessions belonging to OTHER agents in
// the same machine. `weave kill <issue>` reads the recorded
// wrapper PID from the queue and signals only that process group,
// then flips the queue item to `failed` with a "killed by
// orchestrator" marker.
func runWeaveKill(cmd *cobra.Command, id int64, reason string, flags *weaveOutputFlags) error {
	mode := flags.mode()
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave kill",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)
	var killed bool
	var wrapperPid int
	lockErr := withWeaveQueueLock(dir, func(q *weaveQueue) error {
		it := findWeaveItem(q, id)
		if it == nil {
			return fmt.Errorf("issue #%d not found", id)
		}
		if it.State != "working" {
			return fmt.Errorf("issue #%d state is %q (kill requires working)", id, it.State)
		}
		wrapperPid = it.WrapperPid
		if wrapperPid > 0 {
			weaveStopWrapper(wrapperPid)
			killed = true
		}
		it.State = "failed"
		killCode := -1
		it.ExitCode = &killCode
		it.FinishedAt = time.Now().UTC()
		it.WrapperPid = 0
		// Stash the kill reason in the Body so `weave list` shows
		// it (Body isn't load-bearing once the subagent has
		// started — the prompt's already been consumed).
		if reason != "" {
			it.Body = "[killed by orchestrator: " + reason + "]\n\n" + it.Body
		} else {
			it.Body = "[killed by orchestrator]\n\n" + it.Body
		}
		return nil
	})
	if lockErr != nil {
		if strings.Contains(lockErr.Error(), "not found") {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave kill",
				weavecli.ExitInvalidArg, lockErr))
		}
		if strings.Contains(lockErr.Error(), "kill requires working") {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave kill",
				weavecli.ExitStateConflict, lockErr))
		}
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave kill",
			weavecli.ExitGenericFail, lockErr))
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave kill", map[string]any{
			"issue":       id,
			"state":       "failed",
			"wrapper_pid": wrapperPid,
			"killed":      killed,
			"reason":      reason,
		}))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "weave kill: issue #%d wrapper_pid=%d killed=%v state=failed\n", id, wrapperPid, killed)
	return nil
}

// runWeaveWait blocks until issue(s) reach a terminal state
// (submitted, failed, done, abandoned). With --issue N, waits on
// one issue; with --all, waits until no working items remain.
// Times out after timeout (default 1h); on timeout, emits
// precondition_failed and returns ExitPrecondFail so the
// orchestrator can react.
func runWeaveWait(cmd *cobra.Command, issueID int64, all bool, timeout time.Duration, flags *weaveOutputFlags) error {
	mode := flags.mode()
	if !all && issueID <= 0 {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave wait",
			weavecli.ExitInvalidArg, fmt.Errorf("provide --issue N or --all")))
	}
	if all && issueID > 0 {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave wait",
			weavecli.ExitInvalidArg, fmt.Errorf("--issue and --all are mutually exclusive")))
	}
	if timeout <= 0 {
		timeout = time.Hour
	}
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave wait",
			weavecli.ExitPrecondFail, err))
	}
	dir, _ := weaveQueueDir(root)

	type readyItem struct {
		ID       int64  `json:"id"`
		State    string `json:"state"`
		ExitCode *int   `json:"exit_code,omitempty"`
		LogPath  string `json:"log_path,omitempty"`
	}
	deadline := time.Now().Add(timeout)
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		q, err := loadWeaveQueue(dir)
		if err != nil {
			// queue.json may be momentarily absent if reset just ran;
			// surface as ok-empty rather than fail the wait.
			q = &weaveQueue{}
		}
		if issueID > 0 {
			it := findWeaveItem(q, issueID)
			if it == nil {
				return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave wait",
					weavecli.ExitInvalidArg, fmt.Errorf("issue #%d not found", issueID)))
			}
			if isTerminalState(it.State) {
				return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave wait", map[string]any{
					"ready": []readyItem{{ID: it.ID, State: it.State, ExitCode: it.ExitCode, LogPath: it.LogPath}},
				}))
			}
		} else {
			// --all: ready when every non-terminal item is gone.
			// We count both `todo` and `working` as pending so the
			// orchestrator pattern "add N issues → background N
			// `weave start` calls → `weave wait --all`" doesn't
			// race: the wait survives until each todo has been
			// claimed (transitioned through working) and reached
			// a terminal state. If the user has unintended todos
			// that no `start` will ever claim, wait blocks until
			// --timeout — surfacing the gap explicitly rather
			// than silently returning early.
			var ready []readyItem
			pending := 0
			for _, it := range q.Items {
				switch it.State {
				case "todo", "working":
					pending++
				default:
					if isTerminalState(it.State) {
						ready = append(ready, readyItem{ID: it.ID, State: it.State, ExitCode: it.ExitCode, LogPath: it.LogPath})
					}
				}
			}
			if pending == 0 {
				return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave wait", map[string]any{
					"ready": ready,
				}))
			}
		}
		if time.Now().After(deadline) {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave wait",
				weavecli.ExitPrecondFail, fmt.Errorf("timeout after %s", timeout)))
		}
		select {
		case <-ctx.Done():
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave wait",
				weavecli.ExitGenericFail, fmt.Errorf("cancelled")))
		case <-time.After(time.Second):
		}
	}
}
