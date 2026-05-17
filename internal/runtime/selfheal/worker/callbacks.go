package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/autoloop"
	"github.com/qiangli/ycode/internal/runtime/skillengine"
)

// captureSkill registers a CAPTURED skillengine entry on autoloop
// success. The TelemetryTrigger is a quoted-meta regex around the
// brief's normalized error so future occurrences of the same
// signature get a recall hit without false-positive matching on
// random other errors. Idempotent: if a skill with the same name
// already exists, the registry overwrites it with the new stats
// (latest fix wins).
func (w *Worker) captureSkill() error {
	if w.skills == nil {
		return nil
	}
	name := fmt.Sprintf("selfheal-%s", w.brief.Signature)
	spec := &skillengine.SkillSpec{
		Name:              name,
		Version:           1,
		Description:       fmt.Sprintf("Selfheal fix for %s in %s: %s", w.brief.Category, w.brief.Tool, truncate(w.brief.Normalized, 80)),
		Instruction:       fmt.Sprintf("When you see the failure shape recorded in signature %s, the prior fix landed on branch %s in workspace %s. Apply the same approach if applicable.", w.brief.Signature, w.branch, w.layout.Root),
		TelemetryTriggers: []string{regexp.QuoteMeta(w.brief.Normalized)},
		TriggerKeywords:   []string{w.brief.Tool},
		EvolutionMode:     skillengine.EvolutionCaptured,
		Stats: skillengine.SkillStats{
			Uses:         1,
			Successes:    1,
			SuccessRate:  1.0,
			DecayedScore: 1.0,
			LastUsed:     time.Now(),
		},
	}
	return w.skills.Register(spec)
}

// buildCallbacks returns the autoloop callbacks specialized for the
// selfheal use-case. Keeping each callback small and explicit so it's
// obvious what side effects autoloop is triggering on each tick.
func (w *Worker) buildCallbacks() *autoloop.Callbacks {
	return &autoloop.Callbacks{
		Research: w.research,
		Plan:     w.plan,
		Build:    w.build,
		Evaluate: w.evaluate,
		Learn:    w.learn,
	}
}

// research grep's the worktree for the normalized error string so the
// Build subprocess has a starting set of files to inspect. Output is
// the first ~30 hits, formatted as path:line markers — enough context
// for the subsequent Plan/Build steps without overflowing the goal
// description.
func (w *Worker) research(ctx context.Context, _ string) (string, error) {
	needle := bestSearchTerm(w.brief.Normalized)
	if needle == "" {
		return "no searchable term in failure signature; relying on Plan to bootstrap context.", nil
	}
	// Bounded grep: scoped to the worktree, capped at 30 lines.
	subCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(subCtx, "git", "grep", "-n", "--max-count=3", "-F", needle)
	cmd.Dir = w.wtPath
	out, _ := cmd.Output() // non-zero exit when no match — that's fine, returns empty out
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 30 {
		lines = lines[:30]
	}
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return fmt.Sprintf("No grep hits in worktree for %q. Use search tools to locate the failing code path.", needle), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "git grep hits for %q (top %d):\n\n", needle, len(lines))
	for _, line := range lines {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// plan emits a two-task decomposition — regression test first, then
// fix. Deliberately tiny: autoloop's max-iterations bound makes
// expansive task lists wasteful, and the Build subprocess will
// further decompose internally if needed.
func (w *Worker) plan(_ context.Context, _, gapAnalysis string) ([]string, error) {
	tasks := []string{
		fmt.Sprintf("Write a failing regression test for the %s failure in %s. The test should reproduce: %s",
			w.brief.Category, w.brief.Tool, truncate(w.brief.Normalized, 200)),
		fmt.Sprintf("Apply a minimal fix in ycode source so the regression test passes and `%s` still succeeds.",
			w.cfg.EvaluateCommand),
	}
	return tasks, nil
}

// build shells out to a child `ycode prompt` against the worktree.
// The child inherits a curated environment: selfheal-disable +
// signature breadcrumb so the recursion break works and so any
// child-emitted span carries enough context for forensic queries.
// The prompt itself bundles the autoloop goal + research findings +
// task list into a single message — the child decides how to act.
func (w *Worker) build(ctx context.Context, goal string, tasks []string) (int, error) {
	prompt := buildPromptMessage(w.brief, goal, tasks)
	return 1, w.runChildCmd(ctx, w.wtPath, w.cfg.PromptTimeout, w.cfg.YcodeBin, "prompt", prompt)
}

// evaluate runs the configured CI command in the worktree. Returns
// 1.0 on success, 0.0 on failure. Binary score is sufficient for
// stagnation detection: selfheal converges when CI passes.
func (w *Worker) evaluate(ctx context.Context) (float64, error) {
	parts := strings.Fields(w.cfg.EvaluateCommand)
	if len(parts) == 0 {
		return 0, errors.New("evaluate: empty command")
	}
	if err := w.runChildCmd(ctx, w.wtPath, w.cfg.EvalTimeout, parts[0], parts[1:]...); err != nil {
		return 0, nil // failure → score 0 but no Go error; autoloop iterates
	}
	return 1.0, nil
}

// learn appends one JSONL row per iteration to <root>/iterations/
// learnings.jsonl AND, on success (score >= 1.0), registers a
// CAPTURED skill keyed on the brief's normalized error as a
// TelemetryTrigger pattern. Phase 6: future occurrences of the
// same signature will Recall this skill and feed the fix template
// into the goal prompt.
func (w *Worker) learn(_ context.Context, iteration int, score float64) error {
	if err := os.MkdirAll(w.layout.IterPath, 0o755); err != nil {
		return err
	}
	path := filepath.Join(w.layout.IterPath, "learnings.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, `{"ts":%q,"signature":%q,"iteration":%d,"score":%g,"branch":%q}`+"\n",
		time.Now().UTC().Format(time.RFC3339), w.brief.Signature, iteration, score, w.branch); err != nil {
		return err
	}
	// Capture a telemetry-trigger skill on success.
	if score >= 1.0 && w.skills != nil {
		if err := w.captureSkill(); err != nil {
			// Non-fatal: a registry write failure shouldn't fail the
			// autoloop iteration. Surface via slog.
			fmt.Fprintf(os.Stderr, "selfheal worker: capture skill failed: %v\n", err)
		}
	}
	return nil
}

// bestSearchTerm picks the most distinctive substring of the
// normalized error for grep. Prefers the first quoted phrase, falls
// back to the longest non-stopword token. Empty when nothing
// useful — caller handles that.
func bestSearchTerm(normalized string) string {
	if normalized == "" {
		return ""
	}
	// Look for a quoted phrase first — usually the most discriminating.
	if i := strings.IndexAny(normalized, "\"'"); i >= 0 {
		q := normalized[i]
		if j := strings.IndexByte(normalized[i+1:], q); j > 0 {
			cand := normalized[i+1 : i+1+j]
			if len(cand) >= 4 && !looksGeneric(cand) {
				return cand
			}
		}
	}
	// Otherwise: longest "wordy" token, length ≥ 6, not a placeholder.
	best := ""
	for _, tok := range strings.FieldsFunc(normalized, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', ':', ';', ',', '.', '(', ')', '[', ']':
			return true
		}
		return false
	}) {
		if len(tok) < 6 || looksGeneric(tok) {
			continue
		}
		if len(tok) > len(best) {
			best = tok
		}
	}
	return best
}

func looksGeneric(s string) bool {
	switch s {
	case "panic", "error", "failed", "unknown", "missing", "ycode", "<PATH>", "<TS>", "<UUID>", "<HEX>", "<ADDR>", "<LOCALHOST>", "<PORT>", "<L>", "<C>":
		return true
	}
	return strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">")
}

// buildPromptMessage assembles the single message handed to the
// child `ycode prompt` invocation. Front-loads the failure context;
// uses fenced blocks so the child's prompt parser doesn't confuse
// quoted code with instructions.
func buildPromptMessage(b Brief, goal string, tasks []string) string {
	var sb strings.Builder
	sb.WriteString("You are a ycode selfheal worker. A previous tool invocation failed and produced this signature:\n\n")
	fmt.Fprintf(&sb, "- Signature: %s\n", b.Signature)
	fmt.Fprintf(&sb, "- Category: %s\n", b.Category)
	fmt.Fprintf(&sb, "- Tool: %s\n", b.Tool)
	fmt.Fprintf(&sb, "- Scope: %s\n", b.Scope)
	sb.WriteString("\nNormalized error:\n\n```\n")
	sb.WriteString(b.Normalized)
	sb.WriteString("\n```\n\n")
	if b.RawError != "" && b.RawError != b.Normalized {
		sb.WriteString("Raw error (pre-normalization):\n\n```\n")
		sb.WriteString(b.RawError)
		sb.WriteString("\n```\n\n")
	}
	sb.WriteString("Goal:\n\n")
	sb.WriteString(goal)
	sb.WriteString("\n\nTasks:\n")
	for i, t := range tasks {
		fmt.Fprintf(&sb, "  %d. %s\n", i+1, t)
	}
	sb.WriteString("\nConstraints:\n")
	sb.WriteString("  - Do not modify priorart/.\n")
	sb.WriteString("  - Keep the diff small and targeted.\n")
	sb.WriteString("  - Add a regression test before the fix when feasible.\n")
	sb.WriteString("  - Verify with `make ci-fast` before reporting done.\n")
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
