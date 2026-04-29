package agentsmd

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/git"
)

// CommitConvention represents a convention mined from git history.
type CommitConvention struct {
	Pattern   string // the convention pattern
	Frequency int    // how often it appears in fix: commits
	Examples  []string
	Kind      string // "fix-pattern", "hotspot", "convention"
}

// MineCommitConventions analyzes git log to extract implicit conventions
// that agents should know about. Repeated fix: patterns suggest missing guardrails.
func MineCommitConventions(projectRoot string, maxCommits int) ([]CommitConvention, error) {
	if maxCommits <= 0 {
		maxCommits = 200
	}

	var conventions []CommitConvention

	// 1. Mine fix: commits for repeated mistake patterns.
	fixes, err := mineFixPatterns(projectRoot, maxCommits)
	if err == nil {
		conventions = append(conventions, fixes...)
	}

	// 2. Find hotspot directories (most-changed = agents need to know).
	hotspots, err := mineHotspots(projectRoot, maxCommits)
	if err == nil {
		conventions = append(conventions, hotspots...)
	}

	// 3. Extract commit prefix conventions.
	prefixes, err := mineCommitPrefixes(projectRoot, maxCommits)
	if err == nil {
		conventions = append(conventions, prefixes...)
	}

	return conventions, nil
}

// FormatConventions renders mined conventions as markdown suitable for
// appending to an analysis report.
func FormatConventions(conventions []CommitConvention) string {
	if len(conventions) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("### Mined Conventions (from git history)\n\n")

	byKind := map[string][]CommitConvention{}
	for _, c := range conventions {
		byKind[c.Kind] = append(byKind[c.Kind], c)
	}

	kindOrder := []string{"fix-pattern", "hotspot", "convention"}
	kindLabels := map[string]string{
		"fix-pattern": "Repeated Fix Patterns (potential missing guardrails)",
		"hotspot":     "Hotspot Directories (most frequently changed)",
		"convention":  "Commit Conventions",
	}

	for _, kind := range kindOrder {
		items := byKind[kind]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "**%s:**\n", kindLabels[kind])
		for _, c := range items {
			fmt.Fprintf(&b, "- %s (x%d)\n", c.Pattern, c.Frequency)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// --- internals ---

var fixCategoryPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"formatting/linting", regexp.MustCompile(`(?i)(format|lint|fmt|vet|tidy|style)`)},
	{"build/compile", regexp.MustCompile(`(?i)(build|compile|link|binary|codesign)`)},
	{"dependency", regexp.MustCompile(`(?i)(dep|module|import|require|go\.mod|go\.sum)`)},
	{"test", regexp.MustCompile(`(?i)(test|spec|assert|fixture)`)},
	{"config", regexp.MustCompile(`(?i)(config|setting|env|flag|option)`)},
	{"performance", regexp.MustCompile(`(?i)(perf|latency|slow|fast|timeout|memory|leak)`)},
	{"logging/noise", regexp.MustCompile(`(?i)(log|spam|noise|suppress|verbose|stderr)`)},
}

func mineFixPatterns(projectRoot string, maxCommits int) ([]CommitConvention, error) {
	out, err := gitLog(projectRoot, maxCommits)
	if err != nil {
		return nil, err
	}

	// Count fix: commits by category.
	counts := make(map[string]int)
	examples := make(map[string][]string)

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "fix:") && !strings.HasPrefix(strings.ToLower(line), "fix(") {
			continue
		}

		categorized := false
		for _, cat := range fixCategoryPatterns {
			if cat.pattern.MatchString(line) {
				counts[cat.name]++
				if len(examples[cat.name]) < 3 {
					examples[cat.name] = append(examples[cat.name], line)
				}
				categorized = true
				break
			}
		}
		if !categorized {
			counts["other"]++
		}
	}

	var conventions []CommitConvention
	for name, count := range counts {
		if count >= 2 { // Only report patterns that repeat.
			conventions = append(conventions, CommitConvention{
				Pattern:   fmt.Sprintf("%s fixes", name),
				Frequency: count,
				Examples:  examples[name],
				Kind:      "fix-pattern",
			})
		}
	}

	sort.Slice(conventions, func(i, j int) bool {
		return conventions[i].Frequency > conventions[j].Frequency
	})
	return conventions, nil
}

func mineHotspots(projectRoot string, maxCommits int) ([]CommitConvention, error) {
	ge := git.NewGitExec(nil)
	rawOut, err := ge.Run(context.Background(), projectRoot, "log", fmt.Sprintf("-%d", maxCommits),
		"--name-only", "--format=")
	if err != nil {
		return nil, err
	}
	// Count directory-level changes.
	dirCounts := make(map[string]int)
	for _, line := range strings.Split(rawOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Extract top-level directory.
		parts := strings.SplitN(line, "/", 3)
		if len(parts) >= 2 {
			dir := parts[0] + "/" + parts[1]
			// Skip non-source dirs.
			if parts[0] == "priorart" || parts[0] == "external" || parts[0] == ".git" {
				continue
			}
			dirCounts[dir]++
		}
	}

	// Sort by frequency and take top 5.
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range dirCounts {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Value > sorted[j].Value })

	var conventions []CommitConvention
	limit := 5
	if len(sorted) < limit {
		limit = len(sorted)
	}
	for _, kv := range sorted[:limit] {
		conventions = append(conventions, CommitConvention{
			Pattern:   kv.Key,
			Frequency: kv.Value,
			Kind:      "hotspot",
		})
	}
	return conventions, nil
}

func mineCommitPrefixes(projectRoot string, maxCommits int) ([]CommitConvention, error) {
	out, err := gitLog(projectRoot, maxCommits)
	if err != nil {
		return nil, err
	}

	prefixPattern := regexp.MustCompile(`^(\w+)[(:]`)
	counts := make(map[string]int)

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if m := prefixPattern.FindStringSubmatch(line); m != nil {
			counts[strings.ToLower(m[1])]++
		}
	}

	var conventions []CommitConvention
	for prefix, count := range counts {
		if count >= 3 {
			conventions = append(conventions, CommitConvention{
				Pattern:   prefix + ":",
				Frequency: count,
				Kind:      "convention",
			})
		}
	}
	sort.Slice(conventions, func(i, j int) bool {
		return conventions[i].Frequency > conventions[j].Frequency
	})
	return conventions, nil
}

func gitLog(projectRoot string, maxCommits int) (string, error) {
	ge := git.NewGitExec(nil)
	return ge.Run(context.Background(), projectRoot, "log", fmt.Sprintf("-%d", maxCommits), "--format=%s")
}
