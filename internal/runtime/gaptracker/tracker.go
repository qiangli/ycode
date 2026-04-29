// Package gaptracker records capability gaps — operations that ycode delegates
// to external tools instead of handling natively. Gaps are persisted as Gitea
// issues in a dedicated "self-improve" repository, providing a durable backlog
// for the autonomous improvement loop.
//
// The tracker deduplicates by category+subcommand, debounces API calls, and
// operates asynchronously so it never blocks tool execution.
package gaptracker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/runtime/toolexec"
)

const (
	// defaultDebounce is the minimum interval between Gitea API calls
	// for the same gap.
	defaultDebounce = 30 * time.Second

	// labelCapabilityGap is applied to all gap issues.
	labelCapabilityGap = "capability-gap"
)

// gap holds the in-memory state for a single capability gap.
type gap struct {
	Category    string
	Subcommand  string
	Tier        toolexec.Tier
	Count       int64
	FirstSeen   time.Time
	LastSeen    time.Time
	IssueNumber int64 // 0 if not yet created in Gitea
}

// Tracker records capability gaps and persists them as Gitea issues.
type Tracker struct {
	client *gitserver.Client
	owner  string
	repo   string

	seen     map[string]*gap // dedup key → gap
	pending  []*gap          // gaps needing Gitea API calls
	mu       sync.Mutex
	debounce time.Duration

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// New creates a Tracker that persists gaps to the given Gitea repo.
func New(client *gitserver.Client, owner, repo string) *Tracker {
	t := &Tracker{
		client:   client,
		owner:    owner,
		repo:     repo,
		seen:     make(map[string]*gap),
		debounce: defaultDebounce,
		stopCh:   make(chan struct{}),
	}

	// Start background flusher.
	t.wg.Add(1)
	go t.flushLoop()

	return t
}

// RecordGap implements toolexec.GapRecorder.
func (t *Tracker) RecordGap(ctx context.Context, category, subcommand string, tier toolexec.Tier) {
	key := category + ":" + subcommand
	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	if g, ok := t.seen[key]; ok {
		g.Count++
		g.LastSeen = now
		// Upgrade tier if this execution used a higher tier.
		if tier > g.Tier {
			g.Tier = tier
		}
		return
	}

	g := &gap{
		Category:   category,
		Subcommand: subcommand,
		Tier:       tier,
		Count:      1,
		FirstSeen:  now,
		LastSeen:   now,
	}
	t.seen[key] = g
	t.pending = append(t.pending, g)
}

// Bootstrap creates the self-improve repo and labels if needed,
// then loads existing open gap issues into the seen map.
func (t *Tracker) Bootstrap(ctx context.Context) error {
	// Ensure repo exists.
	repos, err := t.client.ListRepos(ctx)
	if err != nil {
		return fmt.Errorf("gaptracker: list repos: %w", err)
	}

	found := false
	for _, r := range repos {
		if r.Name == t.repo {
			found = true
			break
		}
	}

	if !found {
		_, err := t.client.CreateRepo(ctx, t.repo, "Capability gap tracking for self-improvement")
		if err != nil {
			return fmt.Errorf("gaptracker: create repo %s: %w", t.repo, err)
		}
		slog.Info("created self-improve repo", "repo", t.repo)
	}

	// Create labels (ignore errors if they already exist).
	labels := []struct {
		name  string
		color string
	}{
		{labelCapabilityGap, "e11d48"},
		{"tier-2-host-exec", "f59e0b"},
		{"tier-3-container", "ef4444"},
		{"git", "6366f1"},
		{"needs-native-impl", "8b5cf6"},
	}
	for _, l := range labels {
		_, _ = t.client.CreateLabel(ctx, t.owner, t.repo, l.name, l.color)
	}

	// Load existing open gap issues.
	issues, err := t.client.ListIssues(ctx, t.owner, t.repo, "open", []string{labelCapabilityGap})
	if err != nil {
		slog.Warn("gaptracker: failed to load existing issues", "error", err)
		return nil // non-fatal
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, issue := range issues {
		cat, sub := parseIssueTitle(issue.Title)
		if cat == "" {
			continue
		}
		key := cat + ":" + sub
		if _, exists := t.seen[key]; !exists {
			t.seen[key] = &gap{
				Category:    cat,
				Subcommand:  sub,
				IssueNumber: issue.Number,
				FirstSeen:   time.Now(),
				LastSeen:    time.Now(),
			}
		}
	}

	slog.Info("gaptracker bootstrapped", "repo", t.repo, "existing_gaps", len(t.seen))
	return nil
}

// Flush persists all pending gaps to Gitea. Called on graceful shutdown.
func (t *Tracker) Flush(ctx context.Context) error {
	t.mu.Lock()
	pending := t.pending
	t.pending = nil
	t.mu.Unlock()

	for _, g := range pending {
		if err := t.createIssue(ctx, g); err != nil {
			slog.Warn("gaptracker: failed to create issue", "gap", g.Category+":"+g.Subcommand, "error", err)
		}
	}
	return nil
}

// Stop shuts down the background flusher and persists remaining gaps.
func (t *Tracker) Stop(ctx context.Context) {
	t.stopOnce.Do(func() {
		close(t.stopCh)
	})
	t.wg.Wait()
	_ = t.Flush(ctx)
}

// flushLoop periodically flushes pending gaps to Gitea.
func (t *Tracker) flushLoop() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.debounce)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = t.Flush(ctx)
			cancel()
		}
	}
}

// createIssue creates a Gitea issue for a capability gap.
func (t *Tracker) createIssue(ctx context.Context, g *gap) error {
	title := fmt.Sprintf("[capability-gap][%s] %s: no native implementation", g.Category, g.Subcommand)

	tierLabel := "tier-2-host-exec"
	tierDesc := "host binary (system git)"
	if g.Tier == toolexec.TierContainer {
		tierLabel = "tier-3-container"
		tierDesc = "container (no host binary)"
	}

	body := fmt.Sprintf(`## Capability Gap

**Tool:** %s
**Subcommand:** %s
**Execution tier:** %s
**Occurrences:** %d
**First seen:** %s
**Last seen:** %s

## Action

Implement native in-process %s %s support to eliminate external dependency.
`, g.Category, g.Subcommand, tierDesc, g.Count,
		g.FirstSeen.Format(time.DateOnly), g.LastSeen.Format(time.DateOnly),
		g.Category, g.Subcommand)

	labels := []string{labelCapabilityGap, g.Category, tierLabel}

	issue, err := t.client.CreateIssue(ctx, t.owner, t.repo, title, body, labels)
	if err != nil {
		return err
	}

	t.mu.Lock()
	key := g.Category + ":" + g.Subcommand
	if existing, ok := t.seen[key]; ok {
		existing.IssueNumber = issue.Number
	}
	t.mu.Unlock()

	slog.Info("created capability gap issue",
		"issue", issue.Number, "tool", g.Category, "subcommand", g.Subcommand, "tier", g.Tier)
	return nil
}

// parseIssueTitle extracts category and subcommand from a gap issue title.
// Expected format: "[capability-gap][category] subcommand: ..."
func parseIssueTitle(title string) (category, subcommand string) {
	// Look for [capability-gap][category] subcommand:
	if !strings.HasPrefix(title, "[capability-gap][") {
		return "", ""
	}
	rest := title[len("[capability-gap]["):]
	closeBracket := strings.Index(rest, "]")
	if closeBracket < 0 {
		return "", ""
	}
	category = rest[:closeBracket]
	rest = strings.TrimSpace(rest[closeBracket+1:])

	colon := strings.Index(rest, ":")
	if colon < 0 {
		return category, rest
	}
	subcommand = strings.TrimSpace(rest[:colon])
	return category, subcommand
}
