package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/cli/weavecli"
)

const weaveAutopilotLeaseFile = "orchestrator-lease.json"

type weaveAutopilotOptions struct {
	fleetCSV  string
	briefPath string
	standby   bool

	leaseTTL  time.Duration
	heartbeat time.Duration
	backoff   time.Duration
}

type weaveOrchestratorLease struct {
	Holder      string    `json:"holder"`
	Tool        string    `json:"tool"`
	PID         int       `json:"pid"`
	AcquiredAt  time.Time `json:"acquired_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Generation  int64     `json:"generation"`
}

type weaveAutopilotRunner interface {
	Run(ctx context.Context, tool, prompt, queueDir string, onOutput func(string)) (int, error)
	Healthy(ctx context.Context, tool string) bool
}

type weaveExecAutopilotRunner struct{}

var weaveAutopilotRunnerDefault weaveAutopilotRunner = weaveExecAutopilotRunner{}

func runWeaveAutopilot(cmd *cobra.Command, opts weaveAutopilotOptions, flags *weaveOutputFlags) error {
	mode := flags.mode()
	if opts.leaseTTL <= 0 {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave autopilot",
			weavecli.ExitInvalidArg, fmt.Errorf("--lease-ttl must be positive")))
	}
	if opts.heartbeat <= 0 {
		opts.heartbeat = opts.leaseTTL / 3
		if opts.heartbeat <= 0 {
			opts.heartbeat = time.Second
		}
	}
	if opts.backoff <= 0 {
		opts.backoff = 10 * time.Second
	}
	fleet := parseWeaveAutopilotFleet(opts.fleetCSV)
	if len(fleet) == 0 {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave autopilot",
			weavecli.ExitInvalidArg, fmt.Errorf("--orchestrator-fleet is required")))
	}
	cwd, _ := os.Getwd()
	root, err := weaveRepoRoot(cwd)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave autopilot",
			weavecli.ExitPrecondFail, err))
	}
	dir, err := weaveQueueDir(root)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave autopilot",
			weavecli.ExitGenericFail, err))
	}
	brief, err := readWeaveAutopilotBrief(opts.briefPath)
	if err != nil {
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave autopilot",
			weavecli.ExitInvalidArg, err))
	}

	res, err := runWeaveAutopilotLoop(context.Background(), weaveAutopilotLoopOptions{
		queueDir:  dir,
		repoRoot:  root,
		fleet:     fleet,
		brief:     brief,
		standby:   opts.standby,
		leaseTTL:  opts.leaseTTL,
		heartbeat: opts.heartbeat,
		backoff:   opts.backoff,
		stdout:    cmd.OutOrStdout(),
		stderr:    cmd.ErrOrStderr(),
		runner:    weaveAutopilotRunnerDefault,
	})
	if err != nil {
		code := weavecli.ExitGenericFail
		if errors.Is(err, errWeaveAutopilotLeaseBusy) {
			code = weavecli.ExitStateConflict
		}
		return ec(weavecli.EmitError(cmd.ErrOrStderr(), mode, "weave autopilot", code, err))
	}
	if mode == weavecli.OutputJSON {
		return ec(weavecli.EmitOK(cmd.OutOrStdout(), mode, "weave autopilot", res))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "weave autopilot: orchestrator %s exited cleanly\n", res.Tool)
	return nil
}

type weaveAutopilotLoopOptions struct {
	queueDir  string
	repoRoot  string
	fleet     []string
	brief     string
	standby   bool
	leaseTTL  time.Duration
	heartbeat time.Duration
	backoff   time.Duration
	maxRuns   int
	stdout    io.Writer
	stderr    io.Writer
	runner    weaveAutopilotRunner
	now       func() time.Time
	sleep     func(time.Duration)
	holder    string
}

type weaveAutopilotResult struct {
	Tool       string `json:"tool"`
	Runs       int    `json:"runs"`
	QueueDir   string `json:"queue_dir"`
	LastReason string `json:"last_reason,omitempty"`
}

var errWeaveAutopilotLeaseBusy = errors.New("orchestrator lease is held")

func runWeaveAutopilotLoop(ctx context.Context, opts weaveAutopilotLoopOptions) (weaveAutopilotResult, error) {
	if opts.runner == nil {
		opts.runner = weaveAutopilotRunnerDefault
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.sleep == nil {
		opts.sleep = time.Sleep
	}
	if opts.stdout == nil {
		opts.stdout = io.Discard
	}
	if opts.stderr == nil {
		opts.stderr = io.Discard
	}
	if opts.holder == "" {
		opts.holder = weaveAutopilotHolderID()
	}
	if opts.leaseTTL <= 0 {
		opts.leaseTTL = 30 * time.Second
	}
	if opts.heartbeat <= 0 {
		opts.heartbeat = 5 * time.Second
	}
	if opts.backoff <= 0 {
		opts.backoff = 10 * time.Second
	}

	index := 0
	runs := 0
	var lastReason string
	primaryProbeAfter := opts.now()
	for {
		if opts.maxRuns > 0 && runs >= opts.maxRuns {
			return weaveAutopilotResult{Runs: runs, QueueDir: opts.queueDir, LastReason: lastReason}, nil
		}
		tool := opts.fleet[index]
		acquired, lease, err := acquireWeaveAutopilotLease(opts.queueDir, opts.holder, tool, os.Getpid(), opts.leaseTTL, opts.now)
		if err != nil {
			return weaveAutopilotResult{}, err
		}
		if !acquired {
			if !opts.standby {
				return weaveAutopilotResult{}, fmt.Errorf("%w by %s until %s", errWeaveAutopilotLeaseBusy, lease.Holder, lease.ExpiresAt.Format(time.RFC3339))
			}
			wait := lease.ExpiresAt.Sub(opts.now())
			if wait <= 0 {
				wait = opts.heartbeat
			}
			if wait > opts.heartbeat {
				wait = opts.heartbeat
			}
			opts.sleep(wait)
			continue
		}

		leaseLog(opts.queueDir, "takeover", fmt.Sprintf("tool=%s holder=%s generation=%d reason=%s", tool, opts.holder, lease.Generation, lastReasonOrInitial(lastReason)))
		prompt, err := buildWeaveAutopilotPrompt(opts.repoRoot, opts.queueDir, opts.brief)
		if err != nil {
			_ = releaseWeaveAutopilotLease(opts.queueDir, opts.holder)
			return weaveAutopilotResult{}, err
		}

		runCtx, cancel := context.WithCancel(ctx)
		hbDone := make(chan struct{})
		go func() {
			defer close(hbDone)
			ticker := time.NewTicker(opts.heartbeat)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := renewWeaveAutopilotLease(opts.queueDir, opts.holder, tool, opts.leaseTTL, opts.now); err != nil {
						fmt.Fprintf(opts.stderr, "weave autopilot: heartbeat failed: %v\n", err)
					}
				case <-runCtx.Done():
					return
				}
			}
		}()

		overload := false
		exitCode, runErr := opts.runner.Run(runCtx, tool, prompt, opts.queueDir, func(s string) {
			if s == "" {
				return
			}
			_, _ = io.WriteString(opts.stdout, s)
			if weaveAutopilotOverloaded(s) {
				overload = true
				cancel()
			}
		})
		cancel()
		<-hbDone
		runs++

		if overload {
			lastReason = "api-overload signature"
		} else if runErr != nil {
			lastReason = runErr.Error()
		} else if exitCode != 0 {
			lastReason = fmt.Sprintf("exit %d", exitCode)
		} else {
			_ = releaseWeaveAutopilotLease(opts.queueDir, opts.holder)
			return weaveAutopilotResult{Tool: tool, Runs: runs, QueueDir: opts.queueDir}, nil
		}

		leaseLog(opts.queueDir, "failover", fmt.Sprintf("from=%s reason=%s", tool, lastReason))
		_ = releaseWeaveAutopilotLease(opts.queueDir, opts.holder)

		if index == 0 {
			primaryProbeAfter = opts.now().Add(opts.backoff)
		}
		index = (index + 1) % len(opts.fleet)
		if index == 0 {
			opts.sleep(opts.backoff)
			continue
		}
		if opts.now().After(primaryProbeAfter) && opts.runner.Healthy(ctx, opts.fleet[0]) {
			leaseLog(opts.queueDir, "failback", fmt.Sprintf("from=%s to=%s boundary=between-runs", opts.fleet[index], opts.fleet[0]))
			index = 0
		}
	}
}

func parseWeaveAutopilotFleet(s string) []string {
	var fleet []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			fleet = append(fleet, part)
		}
	}
	return fleet
}

func readWeaveAutopilotBrief(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read --brief: %w", err)
	}
	return string(b), nil
}

func weaveAutopilotHolderID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown-host"
	}
	return host + ":" + strconv.Itoa(os.Getpid())
}

func lastReasonOrInitial(reason string) string {
	if reason == "" {
		return "initial"
	}
	return reason
}

func weaveAutopilotLeasePath(dir string) string {
	return filepath.Join(dir, weaveAutopilotLeaseFile)
}

func loadWeaveAutopilotLease(dir string) (weaveOrchestratorLease, bool, error) {
	b, err := os.ReadFile(weaveAutopilotLeasePath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return weaveOrchestratorLease{}, false, nil
	}
	if err != nil {
		return weaveOrchestratorLease{}, false, err
	}
	var l weaveOrchestratorLease
	if err := json.Unmarshal(b, &l); err != nil {
		return weaveOrchestratorLease{}, false, fmt.Errorf("parse orchestrator lease: %w", err)
	}
	return l, true, nil
}

func saveWeaveAutopilotLease(dir string, l weaveOrchestratorLease) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	path := weaveAutopilotLeasePath(dir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func acquireWeaveAutopilotLease(dir, holder, tool string, pid int, ttl time.Duration, now func() time.Time) (bool, weaveOrchestratorLease, error) {
	var out weaveOrchestratorLease
	acquired := false
	err := withWeaveQueueLock(dir, func(q *weaveQueue) error {
		_ = q
		cur, ok, err := loadWeaveAutopilotLease(dir)
		if err != nil {
			return err
		}
		t := now().UTC()
		if ok && cur.Holder != holder && cur.ExpiresAt.After(t) {
			out = cur
			return nil
		}
		gen := cur.Generation + 1
		if !ok {
			gen = 1
		}
		out = weaveOrchestratorLease{
			Holder:      holder,
			Tool:        tool,
			PID:         pid,
			AcquiredAt:  t,
			HeartbeatAt: t,
			ExpiresAt:   t.Add(ttl),
			Generation:  gen,
		}
		if err := saveWeaveAutopilotLease(dir, out); err != nil {
			return err
		}
		acquired = true
		return nil
	})
	return acquired, out, err
}

func renewWeaveAutopilotLease(dir, holder, tool string, ttl time.Duration, now func() time.Time) error {
	return withWeaveQueueLock(dir, func(q *weaveQueue) error {
		_ = q
		cur, ok, err := loadWeaveAutopilotLease(dir)
		if err != nil {
			return err
		}
		if !ok || cur.Holder != holder {
			return fmt.Errorf("orchestrator lease not held by %s", holder)
		}
		t := now().UTC()
		cur.Tool = tool
		cur.HeartbeatAt = t
		cur.ExpiresAt = t.Add(ttl)
		return saveWeaveAutopilotLease(dir, cur)
	})
}

func releaseWeaveAutopilotLease(dir, holder string) error {
	return withWeaveQueueLock(dir, func(q *weaveQueue) error {
		_ = q
		cur, ok, err := loadWeaveAutopilotLease(dir)
		if err != nil {
			return err
		}
		if !ok || cur.Holder != holder {
			return nil
		}
		return os.Remove(weaveAutopilotLeasePath(dir))
	})
}

func buildWeaveAutopilotPrompt(repoRoot, queueDir, brief string) (string, error) {
	q, err := loadWeaveQueue(queueDir)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if strings.TrimSpace(brief) != "" {
		b.WriteString(strings.TrimSpace(brief))
		b.WriteString("\n\n")
	}
	b.WriteString("You are the active ycode weave orchestrator.\n")
	b.WriteString("Resume from durable queue state. Do not assume prior in-memory context survived.\n\n")
	fmt.Fprintf(&b, "Repo root: %s\nQueue dir: %s\n\n", repoRoot, queueDir)
	b.WriteString("At safe top-of-loop boundaries, inspect the queue and run the normal weave gate/merge/launch flow. Never hand off mid-merge.\n\n")
	b.WriteString("Current queue:\n")
	for _, it := range q.Items {
		fmt.Fprintf(&b, "- #%d [%s/%s] %s", it.ID, it.Priority, it.State, it.Title)
		if it.Tool != "" {
			fmt.Fprintf(&b, " tool=%s", it.Tool)
		}
		if it.Sandbox != "" {
			fmt.Fprintf(&b, " sandbox=%s", it.Sandbox)
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func weaveAutopilotOverloaded(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "529") ||
		strings.Contains(low, "overloaded") ||
		strings.Contains(low, "rate limit") ||
		strings.Contains(low, "rate_limit")
}

func leaseLog(dir, event, msg string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	line := fmt.Sprintf("%s %s %s\n", time.Now().UTC().Format(time.RFC3339), event, msg)
	f, err := os.OpenFile(filepath.Join(dir, "autopilot.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func (weaveExecAutopilotRunner) Healthy(ctx context.Context, tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

func (weaveExecAutopilotRunner) Run(ctx context.Context, tool, prompt, queueDir string, onOutput func(string)) (int, error) {
	cmd := exec.CommandContext(ctx, tool)
	cmd.Dir = queueDir
	cmd.Stdin = strings.NewReader(prompt)
	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PWD=") || strings.HasPrefix(kv, "OLDPWD=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, "PWD="+queueDir, "YCODE_WEAVE_ORCHESTRATOR=1", "YCODE_WEAVE_QUEUE_DIR="+queueDir)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 1, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 1, err
	}
	if err := cmd.Start(); err != nil {
		return 1, err
	}

	var wg sync.WaitGroup
	scan := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			onOutput(sc.Text() + "\n")
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	waitErr := cmd.Wait()
	wg.Wait()
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return 1, ctx.Err()
		}
		return 1, waitErr
	}
	return 0, nil
}
