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
}

type weaveItem struct {
	ID       int64     `json:"id"`
	Title    string    `json:"title"`
	Body     string    `json:"body,omitempty"`
	Priority string    `json:"priority,omitempty"`
	State    string    `json:"state"`
	Sandbox  string    `json:"sandbox,omitempty"`
	Branch   string    `json:"branch,omitempty"`
	Created  time.Time `json:"created"`
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
	r := strings.NewReplacer(string(filepath.Separator), "_", ":", "_")
	tag := r.Replace(strings.TrimPrefix(repoRoot, string(filepath.Separator)))
	if len(tag) > 120 {
		tag = tag[len(tag)-120:]
	}
	dir := filepath.Join(home, ".agents", "ycode", "weave", tag)
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
	for _, it := range q.Items {
		if !includeHistory && (it.State == "done" || it.State == "abandoned") {
			continue
		}
		items = append(items, it)
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave list", map[string]any{
			"items": items,
		}))
	}
	if len(items) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "weave list: queue empty")
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%-4s %-4s %-10s %-40s %s\n", "ID", "PRIO", "STATE", "TITLE", "SANDBOX")
	for _, it := range items {
		title := it.Title
		if len(title) > 40 {
			title = title[:37] + "..."
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-4d %-4s %-10s %-40s %s\n", it.ID, it.Priority, it.State, title, it.Sandbox)
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
// in-progress changes.
type weaveStartOptions struct {
	noSpawn bool
	resume  bool
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
		if it.State != "working" || it.Sandbox == "" {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
				weavecli.ExitStateConflict, fmt.Errorf("--resume: issue #%d has no live sandbox (state=%q)", it.ID, it.State)))
		}
		if _, err := os.Stat(it.Sandbox); err != nil {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
				weavecli.ExitStateConflict, fmt.Errorf("--resume: sandbox missing on disk: %s", it.Sandbox)))
		}
	}
	base := weaveBaseBranch(root)
	sandbox := filepath.Join(dir, "sandboxes", fmt.Sprintf("issue-%d", it.ID))
	branch := fmt.Sprintf("agent/weave-issue-%d", it.ID)
	if opts.resume {
		sandbox = it.Sandbox
		branch = it.Branch
	}
	if !opts.resume {
		if _, err := os.Stat(sandbox); err != nil {
			if err := os.MkdirAll(filepath.Dir(sandbox), 0o755); err != nil {
				return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
					weavecli.ExitGenericFail, err))
			}
			gw := exec.Command("git", "-C", root, "worktree", "add", "-b", branch, sandbox, base)
			gw.Stdout = cmd.OutOrStdout()
			gw.Stderr = cmd.ErrOrStderr()
			if err := gw.Run(); err != nil {
				return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
					weavecli.ExitGenericFail, fmt.Errorf("git worktree add: %w", err)))
			}
		}
		for _, kv := range [][2]string{
			{"user.name", fmt.Sprintf("agent-weave-issue-%d", it.ID)},
			{"user.email", fmt.Sprintf("agent-weave-issue-%d@ycode.local", it.ID)},
		} {
			_ = exec.Command("git", "-C", sandbox, "config", kv[0], kv[1]).Run()
		}
		it.State = "working"
		it.Sandbox = sandbox
		it.Branch = branch
		if err := saveWeaveQueue(dir, q); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "weave start: queue write failed (continuing): %v\n", err)
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
	env := os.Environ()
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
	tool.Stdin = os.Stdin
	tool.Stdout = os.Stdout
	tool.Stderr = os.Stderr
	if err := tool.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
				weavecli.ExitGenericFail, fmt.Errorf("tool exited with %d", exit.ExitCode())))
		}
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave start",
			weavecli.ExitGenericFail, err))
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave start", map[string]any{
			"issue":   it.ID,
			"sandbox": sandbox,
			"branch":  branch,
			"state":   "working",
		}))
	}
	return nil
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
		if it.State != "working" {
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
	if it.Sandbox != "" {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", it.Sandbox).Run()
	}
	if it.Branch != "" {
		_ = exec.Command("git", "-C", root, "branch", "-D", it.Branch).Run()
	}
	it.State = "abandoned"
	it.Sandbox = ""
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
			weavecli.ExitDepUnhealthy, fmt.Errorf("--auto requires an LLM provider; not available in the local backend (run via `ycode serve` + Gitea for full v2)")))
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
			"issue":         it.ID,
			"priority":      it.Priority,
			"previous":      prev,
			"title":         it.Title,
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
		if it.Sandbox == "" && it.Branch == "" {
			continue
		}
		if it.Sandbox != "" {
			_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", it.Sandbox).Run()
		}
		if it.Branch != "" {
			_ = exec.Command("git", "-C", root, "branch", "-D", it.Branch).Run()
		}
		teardowns = append(teardowns, tear{Issue: it.ID, Branch: it.Branch, Sandbox: it.Sandbox})
	}
	// Remove the queue file itself; on next add it gets recreated.
	queuePath := filepath.Join(dir, "queue.json")
	_ = os.Remove(queuePath)
	// Best-effort: also remove the sandboxes/ tree in case worktree
	// remove left empty dirs behind.
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
					"issue":              it.ID,
					"sandbox_url":        fileURL,
					"gitea_url":          nil,
					"backend":            "local",
					"note":               "Gitea-backed pages require `ycode serve`; surfacing sandbox path only.",
				}))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "weave open: issue #%d sandbox %s\n", it.ID, fileURL)
			return nil
		}
	}
	_, _, _ = issuesFlag, boardFlag, prFlag
	return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave open",
		weavecli.ExitDepUnhealthy, fmt.Errorf("requires Gitea backend (run `ycode serve` first); local backend has no Gitea pages to open")))
}

// runWeaveInitBoard would create a Gitea kanban project board, but
// the local backend has no Gitea instance. Emit a clean dependency-
// unhealthy envelope so callers know to start `ycode serve`.
func runWeaveInitBoard(cmd *cobra.Command, flags *weaveOutputFlags) error {
	mode := flags.mode()
	return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave init-board",
		weavecli.ExitDepUnhealthy, fmt.Errorf("requires Gitea backend (run `ycode serve` first); local backend has no board to initialize")))
}

// addFromFile parses a markdown checklist or a JSON list and bulk-
// adds each entry to the queue. Returns the IDs created.
//
// Markdown shape (each line, ignoring leading/trailing whitespace):
//
//	- [ ] title goes here
//	- [ ] another title
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
