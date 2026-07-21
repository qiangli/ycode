package docs

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// frontmatterDelim is the YAML frontmatter fence. Both opening and
// closing fences must be exactly `---` on their own line. This matches
// the convention used by every other curated markdown surface in ycode
// (skills, selfinit, etc.) so editors and tooling treat docs the same.
const frontmatterDelim = "---"

// slugPattern restricts topic slugs to a portable, URL-safe form. The
// linter rejects any filename that doesn't match. Keep this strict —
// loosening it (allowing dots, underscores, capitals) breaks the
// `ycode docs <topic>` shell ergonomics and the `--list` JSON contract.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// rawFrontmatter is the shape of the YAML block at the top of every
// agent/*.md file. Keep this struct in lock-step with the documented
// schema in embed.go's safeguard comment.
type rawFrontmatter struct {
	Topic    string `yaml:"topic"`
	Summary  string `yaml:"summary"`
	When     string `yaml:"when"`
	Audience string `yaml:"audience"`
	MaxLines int    `yaml:"max_lines"`
}

// parseDoc consumes the entire raw file contents (including frontmatter)
// for one agent doc and returns the parsed Doc. The expectedSlug is the
// filename minus .md; mismatch with the frontmatter `topic` field is a
// hard error so files can't drift away from their slug silently.
//
// Validation here is the *structural* minimum required to construct a
// Doc. Semantic curation rules (max line count, banned-link patterns,
// required H2 sections) live in lint.go and are enforced by the
// docs_test.go CI gate, not by this function. Keeping parse and lint
// separated means the runtime path never depends on the test data.
func parseDoc(expectedSlug, raw string) (*Doc, error) {
	fmBlock, body, err := splitFrontmatter(raw)
	if err != nil {
		return nil, err
	}

	var fm rawFrontmatter
	if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
		return nil, fmt.Errorf("frontmatter yaml: %w", err)
	}

	if fm.Topic == "" {
		return nil, fmt.Errorf("frontmatter missing required field: topic")
	}
	if fm.Topic != expectedSlug {
		return nil, fmt.Errorf("frontmatter topic %q does not match filename slug %q",
			fm.Topic, expectedSlug)
	}
	if !slugPattern.MatchString(fm.Topic) {
		return nil, fmt.Errorf("topic %q is not a valid slug (must match %s)",
			fm.Topic, slugPattern.String())
	}
	if fm.Summary == "" {
		return nil, fmt.Errorf("frontmatter missing required field: summary")
	}
	if fm.When == "" {
		return nil, fmt.Errorf("frontmatter missing required field: when")
	}
	// `audience: agent` is a tripwire. Its absence flags a doc that was
	// copy-pasted from human docs/ without being re-curated. The linter
	// elevates this from "warn" to "hard fail" — see lint.go.
	if fm.Audience != "agent" {
		return nil, fmt.Errorf(`frontmatter must declare audience: agent (got %q)`, fm.Audience)
	}

	maxLines := fm.MaxLines
	if maxLines == 0 {
		maxLines = DefaultMaxLines
	}
	if maxLines > MaxLinesCap {
		return nil, fmt.Errorf("max_lines=%d exceeds cap %d; split the topic instead",
			maxLines, MaxLinesCap)
	}

	return &Doc{
		Topic:    fm.Topic,
		Summary:  fm.Summary,
		When:     fm.When,
		MaxLines: maxLines,
		Body:     body,
		Raw:      raw,
	}, nil
}

// splitFrontmatter separates the YAML frontmatter block from the
// markdown body. Returns an error if the file does not open with a
// `---` fence — that's the signal that someone added a plain markdown
// file to agent/ without the required schema, which the linter catches.
func splitFrontmatter(raw string) (frontmatter, body string, err error) {
	lines := strings.Split(raw, "\n")
	if len(lines) < 3 || strings.TrimRight(lines[0], "\r") != frontmatterDelim {
		return "", "", fmt.Errorf("missing opening %s frontmatter fence", frontmatterDelim)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == frontmatterDelim {
			fm := strings.Join(lines[1:i], "\n")
			rest := ""
			if i+1 < len(lines) {
				rest = strings.Join(lines[i+1:], "\n")
			}
			return fm, strings.TrimLeft(rest, "\n"), nil
		}
	}
	return "", "", fmt.Errorf("missing closing %s frontmatter fence", frontmatterDelim)
}
