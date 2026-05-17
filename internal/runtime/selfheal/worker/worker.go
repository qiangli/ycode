// Package worker runs the autoloop fix cycle for one selfheal
// signature. Phases 3+4 of the plan
// (/Users/qiangli/.claude/plans/summarize-the-previous-issues-squishy-cupcake.md):
// the worker materializes a per-signature workspace via the workspace
// package, parses the synthesized backlog entry to recover the
// failure context, and drives autoloop.Loop with selfheal-shaped
// Research/Plan/Build/Evaluate/Learn callbacks.
//
// Build delegates to a child `ycode prompt` subprocess against the
// worktree — this reuses the full conversation runtime + tool
// registry without embedding it. The child is spawned with
// YCODE_SELFHEAL_DISABLE=1 so its own selfheal observer never
// recursively tries to fix this worker's failures.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/autoloop"
	"github.com/qiangli/ycode/internal/runtime/git"
	"github.com/qiangli/ycode/internal/runtime/selfheal/workspace"
	"github.com/qiangli/ycode/internal/runtime/skillengine"
)

// Outcome is the persisted summary written to <root>/outcome.json
// when the worker finishes (success or give-up). Phase 5 extends it
// with PR URL / local-only fields.
type Outcome struct {
	Signature    string    `json:"signature"`
	Mode         string    `json:"mode"` // "success" | "gave-up" | "rejected-diff" | "killed"
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	Iterations   int       `json:"iterations"`
	FinalScore   float64   `json:"final_score"`
	DiffLines    int       `json:"diff_lines"`
	BranchName   string    `json:"branch_name,omitempty"`
	WorktreePath string    `json:"worktree_path,omitempty"`
	Notes        string    `json:"notes,omitempty"`
}

// Brief is the parsed view of a selfheal-* backlog entry the worker
// uses to drive autoloop. Populated by ParseBriefFromBacklog.
type Brief struct {
	Signature       string
	Category        string
	Tool            string
	Scope           string
	Normalized      string
	RawError        string
	OccurrenceCount int
	Title           string
}

// Config bounds one worker run.
type Config struct {
	// BaseDir is the per-signature workspace root parent — typically
	// ~/.agents/ycode/selfheal. The signature subdir is created
	// automatically.
	BaseDir string
	// BacklogDir is the per-project backlog directory; the worker
	// finds the entry at <BacklogDir>/selfheal-<sig>-*.md.
	BacklogDir string
	// RepoURL is the ycode source the worker clones into the
	// workspace. Resolved by the daemon via workspace.DiscoverFork.
	RepoURL string
	// MaxIterations bounds the autoloop run. Default 3 — selfheal
	// fixes should be small; runaway loops cost real wall time.
	MaxIterations int
	// DiffLineLimit is the per-iteration safety cap. Diffs larger
	// than this reject the iteration as "likely hallucinated rewrite".
	// Default 500.
	DiffLineLimit int
	// EvaluateCommand is the shell command the Evaluate callback runs
	// in the worktree. Default "make ci-fast" — same gate humans
	// use per the repo's commit policy.
	EvaluateCommand string
	// PromptTimeout caps each Build subprocess.
	PromptTimeout time.Duration
	// EvalTimeout caps each Evaluate subprocess.
	EvalTimeout time.Duration
	// YcodeBin is the path to the ycode binary used for the Build
	// subprocess. Defaults to os.Executable().
	YcodeBin string
	// SkillRegistryDir is where the worker reads + writes
	// telemetry-trigger skills (Phase 6). Empty disables the recall
	// + capture hooks entirely — useful for hermetic tests.
	SkillRegistryDir string
	// Stdout / Stderr for live progress. nil → os.Stdout / os.Stderr.
	Stdout io.Writer
	Stderr io.Writer
}

func (c *Config) applyDefaults() {
	if c.MaxIterations == 0 {
		c.MaxIterations = 3
	}
	if c.DiffLineLimit == 0 {
		c.DiffLineLimit = 500
	}
	if c.EvaluateCommand == "" {
		c.EvaluateCommand = "make ci-fast"
	}
	if c.PromptTimeout == 0 {
		c.PromptTimeout = 30 * time.Minute
	}
	if c.EvalTimeout == 0 {
		c.EvalTimeout = 10 * time.Minute
	}
	if c.Stdout == nil {
		c.Stdout = os.Stdout
	}
	if c.Stderr == nil {
		c.Stderr = os.Stderr
	}
}

// Worker is the long-form fix driver. Cheap to construct; Run does
// the real work.
type Worker struct {
	cfg     Config
	mgr     *workspace.Manager
	exec    *git.GitExec
	brief   Brief
	layout  workspace.Layout
	branch  string
	wtPath  string
	started time.Time
	skills  *skillengine.Registry    // nil when SkillRegistryDir is empty
	recalls []*skillengine.SkillSpec // populated at Run-time from a Recall lookup
}

// New returns a worker for the given signature. The signature must
// match a selfheal-<sig>-*.md backlog entry under cfg.BacklogDir.
func New(cfg Config, signature string) (*Worker, error) {
	cfg.applyDefaults()
	if signature == "" {
		return nil, errors.New("worker: empty signature")
	}
	if cfg.YcodeBin == "" {
		bin, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("worker: locate ycode bin: %w", err)
		}
		cfg.YcodeBin = bin
	}
	brief, err := ParseBriefFromBacklog(cfg.BacklogDir, signature)
	if err != nil {
		return nil, err
	}
	w := &Worker{
		cfg:    cfg,
		mgr:    workspace.New(),
		exec:   git.NewGitExec(nil),
		brief:  brief,
		layout: workspace.PathsFor(cfg.BaseDir, signature),
		branch: fmt.Sprintf("selfheal/%s-%s", signature, time.Now().UTC().Format("20060102t150405")),
	}
	if cfg.SkillRegistryDir != "" {
		reg := skillengine.NewRegistry(cfg.SkillRegistryDir)
		if err := reg.LoadFromDir(); err == nil {
			w.skills = reg
		}
	}
	return w, nil
}

// Run executes the full RESEARCH→PLAN→BUILD→EVALUATE→LEARN cycle and
// writes outcome.json. The returned error reports a fatal setup
// failure; per-iteration evaluation failures land in the Outcome
// struct (mode != "success") rather than as a Go error so the daemon
// can record the result and move on.
func (w *Worker) Run(ctx context.Context) (Outcome, error) {
	w.started = time.Now()
	out := Outcome{
		Signature:  w.brief.Signature,
		Mode:       "gave-up",
		StartedAt:  w.started,
		BranchName: w.branch,
	}

	// Workspace setup: clone + worktree.
	if err := w.setupWorkspace(ctx); err != nil {
		out.FinishedAt = time.Now()
		out.Notes = "setup failed: " + err.Error()
		_ = w.persistOutcome(out)
		return out, err
	}
	out.WorktreePath = w.wtPath

	// Phase 6 recall: query the skill registry for prior fixes
	// whose TelemetryTriggers match this normalized error. Hits
	// are folded into the goal prompt so the autoloop's Build
	// step starts with the prior fix template in context.
	if w.skills != nil {
		w.recalls = w.skills.RecallByTelemetry(w.brief.Normalized)
	}

	cb := w.buildCallbacks()
	loop := autoloop.New(&autoloop.Config{
		Goal:            w.goalSentence(),
		MaxIterations:   w.cfg.MaxIterations,
		StagnationLimit: 1, // selfheal fixes should converge fast
		StateDir:        w.layout.IterPath,
		Timeout:         w.cfg.PromptTimeout + w.cfg.EvalTimeout,
	}, cb)

	results, err := loop.Run(ctx)
	out.Iterations = len(results)
	if len(results) > 0 {
		out.FinalScore = results[len(results)-1].ScoreAfter
	}
	if err != nil {
		out.Notes = "autoloop error: " + err.Error()
	}

	// Diff cap: if Evaluate passed but the diff is huge, reject —
	// likely a hallucinated rewrite rather than a targeted fix.
	if out.FinalScore >= 1.0 {
		lines, err := w.diffLines(ctx)
		out.DiffLines = lines
		if err != nil {
			out.Notes = "diff-line count failed: " + err.Error()
		} else if lines > w.cfg.DiffLineLimit {
			out.Mode = "rejected-diff"
			out.Notes = fmt.Sprintf("diff %d lines > cap %d (likely hallucinated rewrite)", lines, w.cfg.DiffLineLimit)
		} else if lines == 0 {
			out.Mode = "gave-up"
			out.Notes = "no diff produced"
		} else {
			out.Mode = "success"
		}
	}

	out.FinishedAt = time.Now()
	_ = w.persistOutcome(out)
	return out, nil
}

// setupWorkspace materializes the per-signature directory tree, ensures
// the ycode clone is fresh, and adds a worktree on the per-fix branch.
func (w *Worker) setupWorkspace(ctx context.Context) error {
	for _, d := range []string{w.layout.Root, w.layout.WorktreeRoot, w.layout.TracePath, w.layout.IterPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("setup: mkdir %s: %w", d, err)
		}
	}
	if err := w.mgr.EnsureClone(ctx, w.layout, w.cfg.RepoURL); err != nil {
		return fmt.Errorf("setup: clone: %w", err)
	}
	wt, err := w.mgr.CreateWorktree(ctx, w.layout, w.branch)
	if err != nil {
		return fmt.Errorf("setup: worktree: %w", err)
	}
	w.wtPath = wt
	return nil
}

// goalSentence builds the high-level instruction autoloop hands to
// the Build subprocess on every iteration. Designed to be readable
// by a generic ycode prompt session — no selfheal-specific jargon
// the runtime doesn't already understand. When Phase 6 recall hits,
// the prior fix names are appended as a hint block so the LLM has
// the template in context without us re-deriving it.
func (w *Worker) goalSentence() string {
	base := fmt.Sprintf("Fix the failure recorded in this signature.\n\n"+
		"Category: %s\nTool: %s\nScope: %s\nNormalized error: %s\n\n"+
		"Reproduce the failure with a regression test first, then apply a minimal fix in ycode source. "+
		"Run `make ci-fast` to verify. Do not modify priorart/. Keep the diff small.",
		w.brief.Category, w.brief.Tool, w.brief.Scope, w.brief.Normalized)
	if len(w.recalls) > 0 {
		var b strings.Builder
		b.WriteString(base)
		b.WriteString("\n\nPrior fixes for similar failures (skillengine recall):\n")
		for i, s := range w.recalls {
			if i >= 3 {
				break
			}
			fmt.Fprintf(&b, "  - %s (success_rate=%.2f, uses=%d): %s\n",
				s.Name, s.Stats.SuccessRate, s.Stats.Uses, s.Description)
		}
		b.WriteString("\nConsider whether the same approach applies before deriving a new fix.")
		return b.String()
	}
	return base
}

// diffLines counts the lines changed in the worktree relative to its
// branch's upstream tip. Used by the diff-size guardrail.
func (w *Worker) diffLines(ctx context.Context) (int, error) {
	out, err := w.exec.RunOutput(ctx, w.wtPath, "diff", "--numstat", "origin/HEAD")
	if err != nil {
		// Fall back to local-only diff: maybe origin/HEAD doesn't
		// resolve in this fresh clone.
		out, err = w.exec.RunOutput(ctx, w.wtPath, "diff", "--numstat", "HEAD~1")
		if err != nil {
			return 0, err
		}
	}
	total := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: added \t deleted \t path. Binary files use "-".
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		total += atoiOrZero(fields[0]) + atoiOrZero(fields[1])
	}
	return total, nil
}

func atoiOrZero(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func (w *Worker) persistOutcome(out Outcome) error {
	if err := os.MkdirAll(w.layout.Root, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(w.layout.Outcome, b, 0o600)
}

// ParseBriefFromBacklog reads selfheal-<sig>-*.md and pulls the fenced
// YAML block plus the normalized-error section back out into a Brief.
// Tolerates absent fields — the daemon validates the signature exists
// before calling, so partial info is still actionable.
func ParseBriefFromBacklog(backlogDir, signature string) (Brief, error) {
	matches, err := filepath.Glob(filepath.Join(backlogDir, "selfheal-"+signature+"-*.md"))
	if err != nil {
		return Brief{}, err
	}
	if len(matches) == 0 {
		return Brief{}, fmt.Errorf("worker: no backlog entry for signature %s under %s", signature, backlogDir)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return Brief{}, fmt.Errorf("worker: read backlog entry: %w", err)
	}
	b := Brief{Signature: signature}

	// Extract the title from frontmatter (`title: ...`).
	if m := titleRx.FindStringSubmatch(string(data)); len(m) == 2 {
		b.Title = strings.TrimSpace(strings.Trim(m[1], "\""))
	}

	// Parse the ```yaml fenced block.
	if m := yamlBlockRx.FindStringSubmatch(string(data)); len(m) == 2 {
		for _, line := range strings.Split(m[1], "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			switch k {
			case "category":
				b.Category = v
			case "tool":
				b.Tool = v
			case "scope":
				b.Scope = v
			case "occurrence_count":
				b.OccurrenceCount = atoiOrZero(v)
			}
		}
	}

	// Pull the normalized error from the first plain ``` block after
	// the "### Normalized error" header.
	if m := normalizedRx.FindStringSubmatch(string(data)); len(m) == 2 {
		b.Normalized = strings.TrimSpace(m[1])
	}
	if m := rawErrRx.FindStringSubmatch(string(data)); len(m) == 2 {
		b.RawError = strings.TrimSpace(m[1])
	}
	return b, nil
}

var (
	titleRx      = regexp.MustCompile(`(?m)^title:\s*(.+)$`)
	yamlBlockRx  = regexp.MustCompile("(?s)```yaml\\n(.*?)```")
	normalizedRx = regexp.MustCompile("(?s)### Normalized error\\s*\\n+```\\n(.*?)```")
	rawErrRx     = regexp.MustCompile("(?s)### Raw error[^\\n]*\\n+```\\n(.*?)```")
)

// runChildCmd runs a sub-process with selfheal-disable env so the
// child's own observer cannot recurse on this worker's failures.
func (w *Worker) runChildCmd(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) error {
	subCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(subCtx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = w.cfg.Stdout
	cmd.Stderr = w.cfg.Stderr
	cmd.Env = append(os.Environ(),
		"YCODE_SELFHEAL_DISABLE=1",
		"YCODE_SELFHEAL_SIGNATURE="+w.brief.Signature,
	)
	return cmd.Run()
}
