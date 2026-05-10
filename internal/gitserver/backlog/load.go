//go:build experimental

package backlog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// frontmatterDoc is the on-disk YAML schema. Field names are lowercase
// because Go's yaml.v3 default-lowers struct field names; we pin them
// explicitly with tags so renames don't silently change the file format.
type frontmatterDoc struct {
	Title      string    `yaml:"title"`
	Priority   string    `yaml:"priority"`
	State      string    `yaml:"state"`
	Created    time.Time `yaml:"created"`
	GiteaIssue *int64    `yaml:"gitea_issue,omitempty"`
	Acceptance []string  `yaml:"acceptance,omitempty"`
}

const (
	fmDelim       = "---\n"
	pauseSentinel = "PAUSE"
)

// Load reads all *.md files under dir and returns them sorted by
// (priority asc, created asc). Files that fail to parse are skipped
// with a non-fatal error returned alongside the partial list.
func Load(dir string) ([]Issue, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backlog: read dir %s: %w", dir, err)
	}
	out := make([]Issue, 0, len(entries))
	var firstErr error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		// README.md and the PAUSE sentinel are not issues.
		if e.Name() == "README.md" || e.Name() == pauseSentinel {
			continue
		}
		full := filepath.Join(dir, e.Name())
		issue, err := ParseFile(full)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, issue)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return priorityRank(out[i].Priority) < priorityRank(out[j].Priority)
		}
		return out[i].Created.Before(out[j].Created)
	})
	return out, firstErr
}

// ParseFile reads one markdown file with YAML frontmatter and returns
// an Issue. Returns an error if the frontmatter is missing, malformed,
// or fails validation.
func ParseFile(path string) (Issue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Issue{}, fmt.Errorf("backlog: read %s: %w", path, err)
	}
	fm, body, err := splitFrontmatter(data)
	if err != nil {
		return Issue{}, fmt.Errorf("backlog: %s: %w", path, err)
	}
	var doc frontmatterDoc
	if err := yaml.Unmarshal(fm, &doc); err != nil {
		return Issue{}, fmt.Errorf("backlog: %s: parse frontmatter: %w", path, err)
	}
	if doc.Priority == "" {
		doc.Priority = PriorityP2
	}
	if doc.State == "" {
		doc.State = StateOpen
	}
	if !IsValidPriority(doc.Priority) {
		return Issue{}, fmt.Errorf("backlog: %s: invalid priority %q", path, doc.Priority)
	}
	if !IsValidState(doc.State) {
		return Issue{}, fmt.Errorf("backlog: %s: invalid state %q", path, doc.State)
	}
	slug := strings.TrimSuffix(filepath.Base(path), ".md")
	if doc.Title == "" {
		return Issue{}, fmt.Errorf("backlog: %s: missing title", path)
	}
	if doc.Created.IsZero() {
		doc.Created = time.Now().UTC()
	}
	return Issue{
		Slug:       slug,
		Title:      doc.Title,
		Priority:   doc.Priority,
		State:      doc.State,
		Created:    doc.Created,
		GiteaIssue: doc.GiteaIssue,
		Acceptance: doc.Acceptance,
		Body:       string(body),
		Path:       path,
	}, nil
}

// WriteFile renders an Issue back to disk at the given path (or
// issue.Path if empty), atomically via temp+rename.
func WriteFile(issue Issue, path string) error {
	if path == "" {
		path = issue.Path
	}
	if path == "" {
		return fmt.Errorf("backlog: WriteFile: no path")
	}
	rendered, err := render(issue)
	if err != nil {
		return err
	}
	return atomicWrite(path, rendered)
}

// MarkState rewrites the `state:` field of <dir>/<slug>.md atomically.
// Other frontmatter fields and the body are preserved byte-for-byte.
func MarkState(dir, slug, state string) error {
	if !IsValidState(state) {
		return fmt.Errorf("backlog: invalid state %q", state)
	}
	path := filepath.Join(dir, slug+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("backlog: MarkState read %s: %w", path, err)
	}
	updated, err := rewriteFrontmatterField(data, "state", state)
	if err != nil {
		return fmt.Errorf("backlog: MarkState %s: %w", path, err)
	}
	return atomicWrite(path, updated)
}

// SetGiteaIssue rewrites the `gitea_issue:` field of <dir>/<slug>.md.
// Used by reconcile after a successful Submit.
func SetGiteaIssue(dir, slug string, number int64) error {
	path := filepath.Join(dir, slug+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("backlog: SetGiteaIssue read %s: %w", path, err)
	}
	updated, err := rewriteFrontmatterField(data, "gitea_issue", fmt.Sprintf("%d", number))
	if err != nil {
		return fmt.Errorf("backlog: SetGiteaIssue %s: %w", path, err)
	}
	return atomicWrite(path, updated)
}

// SetPriority rewrites the `priority:` field. Used by `ycode foreman prio`.
func SetPriority(dir, slug, priority string) error {
	if !IsValidPriority(priority) {
		return fmt.Errorf("backlog: invalid priority %q", priority)
	}
	path := filepath.Join(dir, slug+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("backlog: SetPriority read %s: %w", path, err)
	}
	updated, err := rewriteFrontmatterField(data, "priority", priority)
	if err != nil {
		return fmt.Errorf("backlog: SetPriority %s: %w", path, err)
	}
	return atomicWrite(path, updated)
}

// RenderGiteaBody returns the body string to push to Gitea for this
// issue: a slug marker on line 1, then the freeform body, then an
// optional Acceptance section.
func RenderGiteaBody(issue Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s%s\n\n", SlugMarkerPrefix, issue.Slug, SlugMarkerSuffix)
	b.WriteString(strings.TrimSpace(issue.Body))
	if len(issue.Acceptance) > 0 {
		b.WriteString("\n\n## Acceptance\n\n")
		for _, item := range issue.Acceptance {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// SlugFromGiteaBody extracts the slug from a Gitea body that was
// rendered by RenderGiteaBody. Returns "" if the marker is missing.
func SlugFromGiteaBody(body string) string {
	body = strings.TrimLeft(body, "\r\n ")
	if !strings.HasPrefix(body, SlugMarkerPrefix) {
		return ""
	}
	rest := body[len(SlugMarkerPrefix):]
	end := strings.Index(rest, SlugMarkerSuffix)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// PauseSentinelExists reports whether docs/backlog/PAUSE is present.
func PauseSentinelExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, pauseSentinel))
	return err == nil
}

// --- internals ---

func splitFrontmatter(data []byte) (fm, body []byte, err error) {
	// Accept both "---\n" and "---\r\n" as the leading delimiter.
	if !bytes.HasPrefix(data, []byte("---")) {
		return nil, nil, fmt.Errorf("missing frontmatter delimiter")
	}
	// Skip the opening "---" line.
	nl := bytes.IndexByte(data, '\n')
	if nl < 0 {
		return nil, nil, fmt.Errorf("missing frontmatter delimiter")
	}
	rest := data[nl+1:]
	// Find the closing "---" on its own line.
	idx := bytes.Index(rest, []byte("\n---"))
	if idx < 0 {
		return nil, nil, fmt.Errorf("missing closing frontmatter delimiter")
	}
	fm = rest[:idx]
	tail := rest[idx+1:] // starts with "---"
	// Skip past the closing delimiter line.
	nl = bytes.IndexByte(tail, '\n')
	if nl < 0 {
		body = nil
	} else {
		body = tail[nl+1:]
	}
	return fm, body, nil
}

// rewriteFrontmatterField replaces the value of a top-level frontmatter
// key, or appends a new line if the key is absent. Body and other
// fields are preserved byte-for-byte. The value is YAML-encoded as a
// scalar (quoted only if necessary).
func rewriteFrontmatterField(data []byte, key, value string) ([]byte, error) {
	fm, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(fm), "\n")
	prefix := key + ":"
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			lines[i] = key + ": " + yamlScalar(value)
			replaced = true
			break
		}
	}
	if !replaced {
		// Insert before the trailing blank line if any, else append.
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = append(lines[:len(lines)-1], key+": "+yamlScalar(value), "")
		} else {
			lines = append(lines, key+": "+yamlScalar(value))
		}
	}
	var out bytes.Buffer
	out.WriteString(fmDelim)
	out.WriteString(strings.Join(lines, "\n"))
	if !strings.HasSuffix(out.String(), "\n") {
		out.WriteString("\n")
	}
	out.WriteString("---\n")
	out.Write(body)
	return out.Bytes(), nil
}

// yamlScalar returns a YAML-safe scalar rendering of v. Wraps in
// double quotes when v contains characters that would require it.
func yamlScalar(v string) string {
	if v == "" {
		return `""`
	}
	// Numeric, boolean, and bare-safe identifiers can go unquoted.
	for _, r := range v {
		if r == ':' || r == '#' || r == '\'' || r == '"' || r == '\n' || r == '\r' || r == '\t' {
			b, _ := yaml.Marshal(v)
			return strings.TrimRight(string(b), "\n")
		}
	}
	return v
}

// render serializes an Issue to bytes (frontmatter + body).
func render(issue Issue) ([]byte, error) {
	doc := frontmatterDoc{
		Title:      issue.Title,
		Priority:   issue.Priority,
		State:      issue.State,
		Created:    issue.Created,
		GiteaIssue: issue.GiteaIssue,
		Acceptance: issue.Acceptance,
	}
	fmBytes, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(fmDelim)
	b.Write(fmBytes)
	b.WriteString("---\n")
	body := strings.TrimSpace(issue.Body)
	if body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func priorityRank(p string) int {
	switch p {
	case PriorityP1:
		return 1
	case PriorityP2:
		return 2
	case PriorityP3:
		return 3
	}
	return 9
}
