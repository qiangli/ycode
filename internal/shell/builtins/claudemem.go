package builtins

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/qiangli/ycode/pkg/memex/memory"
)

// claudeMemoryDir returns the per-project Claude Code memory dir,
// `~/.claude/projects/<project-id>/memory/`, where <project-id> is the
// cwd with path separators replaced by '-'. Returns "" when:
//   - $CLAUDE_PROJECT_DIR is set to a path that doesn't exist (explicit
//     opt-out), or
//   - the directory does not exist (Claude isn't using this project).
//
// We never create it: only write through when Claude has already
// initialized the memory layout for this project.
func claudeMemoryDir() string {
	if v := strings.TrimSpace(os.Getenv("CLAUDE_PROJECT_DIR")); v != "" {
		if fi, err := os.Stat(v); err != nil || !fi.IsDir() {
			return ""
		}
		dir := filepath.Join(v, "memory")
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	id := strings.ReplaceAll(cwd, string(os.PathSeparator), "-")
	dir := filepath.Join(home, ".claude", "projects", id, "memory")
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir
	}
	return ""
}

var memSlugRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// writeThroughClaudeMemory writes a Claude-format markdown file plus an
// index line in MEMORY.md when Claude memory dir is available. Returns
// the relative path of the written file (for the user-facing acknowledgement)
// or "" when no bridge was performed.
func writeThroughClaudeMemory(mem *memory.Memory) (string, error) {
	dir := claudeMemoryDir()
	if dir == "" {
		return "", nil
	}
	slug := strings.ToLower(mem.Name)
	slug = memSlugRe.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "note"
	}
	filename := fmt.Sprintf("%s_%s.md", strings.ToLower(string(mem.Type)), slug)
	path := filepath.Join(dir, filename)

	body := fmt.Sprintf(`---
name: %s
description: %s
type: %s
---

%s
`, mem.Name, escapeYAMLOneLine(mem.Description), mem.Type, mem.Content)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", fmt.Errorf("write claude memory: %w", err)
	}

	// Append a one-line index entry to MEMORY.md (create if missing).
	idxPath := filepath.Join(dir, "MEMORY.md")
	indexLine := fmt.Sprintf("- [%s](%s) — %s\n", mem.Name, filename, firstLine(mem.Description, 120))
	if existing, err := os.ReadFile(idxPath); err == nil {
		if strings.Contains(string(existing), "("+filename+")") {
			// Already indexed (idempotent update of same entry); skip append.
			return path, nil
		}
		f, ferr := os.OpenFile(idxPath, os.O_APPEND|os.O_WRONLY, 0o600)
		if ferr != nil {
			return path, fmt.Errorf("open MEMORY.md: %w", ferr)
		}
		defer f.Close()
		if !strings.HasSuffix(string(existing), "\n") {
			_, _ = f.WriteString("\n")
		}
		_, _ = f.WriteString(indexLine)
		return path, nil
	}
	// MEMORY.md doesn't exist — create with this line as the first entry.
	if err := os.WriteFile(idxPath, []byte(indexLine), 0o600); err != nil {
		return path, fmt.Errorf("create MEMORY.md: %w", err)
	}
	return path, nil
}

// scanClaudeMemory does a substring match across the Claude memory dir
// and returns matching entries as SearchResult values. Score is the
// fraction of file size occupied by the longest match (a coarse proxy
// for relevance) clamped to [0.1, 0.9] so memex's RRF-scored hits
// remain comparable but Claude hits still surface.
func scanClaudeMemory(query string, limit int) []memory.SearchResult {
	dir := claudeMemoryDir()
	if dir == "" || strings.TrimSpace(query) == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	qLower := strings.ToLower(query)
	parts := strings.Fields(qLower)
	var out []memory.SearchResult
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.EqualFold(e.Name(), "MEMORY.md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(raw)
		low := strings.ToLower(text)
		score := 0.0
		for _, p := range parts {
			if p != "" && strings.Contains(low, p) {
				score += float64(len(p)) / float64(len(low)+1)
			}
		}
		if score == 0 {
			continue
		}
		if score > 0.9 {
			score = 0.9
		}
		if score < 0.1 {
			score = 0.1
		}
		name, desc, mtype, content := parseClaudeFrontmatter(text, e.Name())
		out = append(out, memory.SearchResult{
			Memory: &memory.Memory{
				Name:        name,
				Description: desc,
				Type:        memory.Type(mtype),
				Content:     content,
			},
			Score:  score,
			Source: "claude",
		})
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// parseClaudeFrontmatter extracts name/description/type fields from the
// YAML frontmatter at the top of a Claude memory file. Returns sensible
// fallbacks when fields are missing.
func parseClaudeFrontmatter(text, filename string) (name, desc, mtype, content string) {
	name = strings.TrimSuffix(filename, ".md")
	mtype = "reference"
	content = text
	if !strings.HasPrefix(text, "---") {
		return
	}
	end := strings.Index(text[3:], "---")
	if end < 0 {
		return
	}
	header := text[3 : 3+end]
	content = strings.TrimLeft(text[3+end+3:], "\n")
	for line := range strings.SplitSeq(header, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		switch key {
		case "name":
			if val != "" {
				name = val
			}
		case "description":
			desc = val
		case "type":
			if val != "" {
				mtype = val
			}
		}
	}
	return
}

// escapeYAMLOneLine produces a YAML-safe one-line value. The memory
// description is constrained to a single line elsewhere; this guards
// against accidental colons or # breaking the frontmatter.
func escapeYAMLOneLine(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#\"'\n") {
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, `"`, `\"`)
		return `"` + strings.ReplaceAll(s, "\n", " ") + `"`
	}
	return s
}
