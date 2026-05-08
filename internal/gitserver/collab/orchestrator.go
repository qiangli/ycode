// Package collab implements the autopilot collab orchestrator —
// the daemon that runs N child autopilot processes in parallel,
// each working a popped task in an isolated fork checkout, while
// the merger auto-merges green PRs. See docs/agent-collab.md.
package collab

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/agents"
	"github.com/qiangli/ycode/internal/gitserver/merger"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/gitserver/queue"
)

// Config wires an Orchestrator to its dependencies. All fields are
// required unless marked optional.
type Config struct {
	Project *projects.Project
	Client  *gitserver.Client
	SyncLog *projects.SyncLog

	NumAgents int
	CICommand string // empty = auto-merge unconditionally (dangerous in prod)

	// YcodeBin is the path to the ycode binary used to spawn child
	// autopilot runs. Defaults to os.Args[0].
	YcodeBin string

	// SandboxRoot is the parent dir for per-agent fork checkouts.
	// Default: <giteaDataDir>/collab-sandboxes/
	SandboxRoot string

	// SessionsRoot is where per-agent autopilot run logs go.
	// Default: <giteaDataDir>/collab-sessions/
	SessionsRoot string

	// IssueTimeout caps wall-clock time per autopilot run. Default 30 min.
	IssueTimeout time.Duration

	// PollInterval — agents and merger poll the queue/PR list this often.
	// Default 10 seconds.
	PollInterval time.Duration

	Token    string
	CloneURL string

	// HostCwd is the user's working tree (NOT the sandbox). Used for
	// the merger's push:origin post-merge action — it pushes merged
	// SHAs to whatever "origin" remote is configured in cwd.
	HostCwd string

	Logger *slog.Logger
}

// Orchestrator runs the autopilot collab loop for one project.
type Orchestrator struct {
	cfg     Config
	metrics *Metrics
}

// New validates and constructs an Orchestrator.
func New(cfg Config) (*Orchestrator, error) {
	if cfg.Project == nil {
		return nil, errors.New("collab: nil Project")
	}
	if cfg.Client == nil {
		return nil, errors.New("collab: nil Client")
	}
	if cfg.SyncLog == nil {
		return nil, errors.New("collab: nil SyncLog")
	}
	if cfg.NumAgents <= 0 {
		cfg.NumAgents = 1
	}
	if cfg.YcodeBin == "" {
		cfg.YcodeBin = os.Args[0]
	}
	if cfg.IssueTimeout <= 0 {
		cfg.IssueTimeout = 30 * time.Minute
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 10 * time.Second
	}
	if cfg.Token == "" {
		return nil, errors.New("collab: empty Token")
	}
	if cfg.CloneURL == "" {
		return nil, errors.New("collab: empty CloneURL")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.SandboxRoot == "" {
		return nil, errors.New("collab: empty SandboxRoot")
	}
	if cfg.SessionsRoot == "" {
		return nil, errors.New("collab: empty SessionsRoot")
	}

	m, _ := newMetrics() // metrics failure is best-effort, not fatal

	return &Orchestrator{cfg: cfg, metrics: m}, nil
}

// Run launches NumAgents agent goroutines + the merger + the
// queue-depth reporter. Blocks until ctx is canceled.
func (o *Orchestrator) Run(ctx context.Context) error {
	if err := queue.EnsureLabels(ctx, o.cfg.Client, o.cfg.Project); err != nil {
		return fmt.Errorf("collab.Run: ensure labels: %w", err)
	}

	var wg sync.WaitGroup

	// Merger goroutine.
	mergerCfg := merger.Config{
		Client:    o.cfg.Client,
		Project:   o.cfg.Project,
		SyncLog:   o.cfg.SyncLog,
		CloneURL:  o.cfg.CloneURL,
		Token:     o.cfg.Token,
		CICommand: o.cfg.CICommand,
		WorkDir:   filepath.Join(filepath.Dir(o.cfg.SandboxRoot), "merger-work"),
		Logger:    o.cfg.Logger.With("component", "merger"),
	}
	if o.cfg.HostCwd != "" {
		mergerCfg.OriginPushFn = o.makeOriginPushFn()
	}
	m, err := merger.New(mergerCfg)
	if err != nil {
		return fmt.Errorf("collab.Run: merger: %w", err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = m.Run(ctx, o.cfg.PollInterval)
	}()

	// Queue-depth reporter goroutine — periodically updates the
	// ycode_tasks_queue_depth gauge.
	wg.Add(1)
	go func() {
		defer wg.Done()
		o.reportQueueDepth(ctx)
	}()

	// Agent goroutines.
	for i := 0; i < o.cfg.NumAgents; i++ {
		a := agents.NewAgent(fmt.Sprintf("worker-%d", i+1))
		wg.Add(1)
		go func(a *agents.Agent) {
			defer wg.Done()
			o.agentLoop(ctx, a)
		}(a)
	}

	wg.Wait()
	return ctx.Err()
}

// agentLoop is the per-agent main loop. Runs until ctx is canceled.
func (o *Orchestrator) agentLoop(ctx context.Context, a *agents.Agent) {
	log := o.cfg.Logger.With("agent.id", a.ID)
	tracer := otel.Tracer("ycode.collab")

	// Attach agent baggage so downstream child-process invocations
	// (and any spans inside this goroutine) carry the identity.
	if bag, err := baggage.New(
		mustMember("agent.id", a.ID),
		mustMember("project.slug", o.cfg.Project.Slug),
	); err == nil {
		ctx = baggage.ContextWithBaggage(ctx, bag)
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		issue, err := queue.Pop(ctx, o.cfg.Client, o.cfg.Project, a.ID)
		if err != nil {
			log.Warn("collab: queue.Pop", "err", err)
			sleepCtx(ctx, o.cfg.PollInterval)
			continue
		}
		if issue == nil {
			sleepCtx(ctx, o.cfg.PollInterval)
			continue
		}

		iterCtx, span := tracer.Start(ctx, "ycode.collab.iteration",
			trace.WithAttributes(
				attribute.String("agent.id", a.ID),
				attribute.String("project.slug", o.cfg.Project.Slug),
				attribute.Int64("task.issue_num", issue.Number),
			),
		)
		o.processIssue(iterCtx, log, a, issue)
		span.End()

		o.metrics.recordIteration(ctx, a.ID, o.cfg.Project.Slug)
	}
}

// processIssue runs one full Pop→Sandbox→Autopilot→Push→PR cycle.
// On any failure, releases the issue back to the queue.
func (o *Orchestrator) processIssue(ctx context.Context, log *slog.Logger, a *agents.Agent, issue *gitserver.Issue) {
	tracer := otel.Tracer("ycode.collab")
	branch := agents.BranchName(a, issue.Number)

	// Sandbox prep.
	sandboxCtx, sandboxSpan := tracer.Start(ctx, "ycode.collab.sandbox.prep")
	sandbox, err := PrepareSandbox(sandboxCtx, o.cfg.SandboxRoot, o.cfg.CloneURL, o.cfg.Token, a, issue.Number, branch)
	sandboxSpan.End()
	if err != nil {
		log.Warn("collab: sandbox prep failed; releasing", "issue", issue.Number, "err", err)
		o.releaseAndCount(ctx, a, issue.Number, "abandoned")
		return
	}

	// Spawn the autopilot child.
	autopilotCtx, autopilotSpan := tracer.Start(ctx, "ycode.collab.autopilot.run")
	exitErr := o.runAutopilot(autopilotCtx, log, a, sandbox, issue)
	autopilotSpan.End()
	if exitErr != nil {
		log.Warn("collab: autopilot child failed; releasing", "issue", issue.Number, "err", exitErr)
		o.releaseAndCount(ctx, a, issue.Number, "abandoned")
		return
	}

	// Branch was assigned during sandbox prep (locally created on the
	// clone). Now publish to Gitea by creating the remote branch first
	// (idempotent — Gitea returns 409 if it exists, which AssignBranch
	// already handles silently).
	br, err := agents.AssignBranch(ctx, o.cfg.Client, o.cfg.Project, a, issue.Number)
	if err != nil {
		log.Warn("collab: AssignBranch on Gitea failed; releasing", "err", err)
		o.releaseAndCount(ctx, a, issue.Number, "abandoned")
		return
	}

	// Push the agent's local branch to Gitea.
	if err := br.Push(ctx, agents.PushOptions{
		WorktreePath: sandbox,
		CloneURL:     o.cfg.CloneURL,
		Token:        o.cfg.Token,
		Force:        true,
	}); err != nil {
		log.Warn("collab: push failed; releasing", "err", err)
		o.releaseAndCount(ctx, a, issue.Number, "abandoned")
		return
	}

	// Open PR. The merger picks it up on its next tick.
	pr, err := br.OpenPR(ctx, o.cfg.Client, "", "")
	if err != nil {
		log.Warn("collab: OpenPR failed; releasing", "err", err)
		o.releaseAndCount(ctx, a, issue.Number, "abandoned")
		return
	}

	log.Info("collab: PR opened", "pr", pr.Number, "issue", issue.Number, "branch", br.Name)
}

// runAutopilot spawns `<ycodeBin> prompt /autopilot collab task <title>\n\n<body>`
// inside the sandbox dir, capturing combined output to a per-agent log
// file. Returns nil on exit code 0.
func (o *Orchestrator) runAutopilot(ctx context.Context, log *slog.Logger, a *agents.Agent, sandbox string, issue *gitserver.Issue) error {
	logDir := filepath.Join(o.cfg.SessionsRoot, a.ID)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("collab: mkdir log dir: %w", err)
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("issue-%d.log", issue.Number))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("collab: open log: %w", err)
	}
	defer logFile.Close()

	prompt := fmt.Sprintf("/autopilot collab task %s\n\n%s", issue.Title, issue.Body)
	cmdCtx, cancel := context.WithTimeout(ctx, o.cfg.IssueTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, o.cfg.YcodeBin, "prompt", prompt)
	cmd.Dir = sandbox
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = os.Environ()

	log.Info("collab: spawning autopilot", "issue", issue.Number, "log", logPath)
	if err := cmd.Run(); err != nil {
		// Distinguish timeout from non-zero exit, but treat both as failure.
		if cmdCtx.Err() != nil {
			return fmt.Errorf("collab: autopilot timeout after %s", o.cfg.IssueTimeout)
		}
		return fmt.Errorf("collab: autopilot exit: %w", err)
	}
	return nil
}

// reportQueueDepth periodically updates the queue depth gauge.
func (o *Orchestrator) reportQueueDepth(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()

	tick := func() {
		issues, err := queue.List(ctx, o.cfg.Client, o.cfg.Project, "open")
		if err != nil {
			return
		}
		counts := map[string]int64{
			queue.LabelP1: 0,
			queue.LabelP2: 0,
			queue.LabelP3: 0,
		}
		for _, i := range issues {
			counts[queue.Priority(&i)]++
		}
		for prio, n := range counts {
			o.metrics.setQueueDepth(ctx, o.cfg.Project.Slug, prio, n)
		}
	}
	tick() // immediate baseline
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

// releaseAndCount returns the issue to the queue and bumps the
// failure-status PR counter for OTEL.
func (o *Orchestrator) releaseAndCount(ctx context.Context, a *agents.Agent, issueNo int64, status string) {
	if err := queue.Release(ctx, o.cfg.Client, o.cfg.Project, issueNo, a.ID); err != nil {
		o.cfg.Logger.Warn("collab: Release failed", "issue", issueNo, "err", err)
	}
	o.metrics.recordPR(ctx, a.ID, status)
}

// makeOriginPushFn returns a function that pushes merged SHAs to the
// host repo's "origin" remote, used by the merger when an issue carries
// the push:origin label.
func (o *Orchestrator) makeOriginPushFn() func(context.Context, string) error {
	hostCwd := o.cfg.HostCwd
	return func(ctx context.Context, sha string) error {
		// Push the merged commit to "origin/main" in the user's working
		// tree's remote configuration. Do not modify cwd — the user's
		// "origin" already points at GitHub (or wherever).
		cmd := exec.CommandContext(ctx, "git", "-C", hostCwd, "push", "origin", sha+":refs/heads/main")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("collab: push:origin: %w\n%s", err, string(out))
		}
		return nil
	}
}

// --- helpers ---

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func mustMember(key, val string) baggage.Member {
	m, _ := baggage.NewMember(key, val)
	return m
}
