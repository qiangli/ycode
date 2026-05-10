//go:build experimental

// DOM compression — when the action returns a large extracted page,
// strip the parts that have no agent value and dedupe sibling
// boilerplate. Modeled after openchrome's (MIT) 15× DOM compression
// at a simpler scale: we ship a starter set of rules that handle
// the common offenders (scripts, styles, SVG, deeply repeated
// classes). A second pass collapses whitespace.

package reliability

import (
	"context"
	"regexp"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

type compactDOMWrapper struct {
	inner mcpservers.Service
}

func (c *compactDOMWrapper) Name() string                       { return c.inner.Name() }
func (c *compactDOMWrapper) Available(ctx context.Context) bool { return c.inner.Available(ctx) }
func (c *compactDOMWrapper) EnsureReady(ctx context.Context) error {
	return c.inner.EnsureReady(ctx)
}
func (c *compactDOMWrapper) Stop(ctx context.Context) error { return c.inner.Stop(ctx) }

func (c *compactDOMWrapper) Execute(ctx context.Context, action mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	res, err := c.inner.Execute(ctx, action)
	if err != nil || res == nil {
		return res, err
	}
	// Compress only the body of extract/navigate actions, where
	// page content dominates the token cost.
	switch action.Type {
	case mcpservers.ActionExtract, mcpservers.ActionNavigate:
		res.Content = compactText(res.Content)
	}
	return res, nil
}

var (
	reScript    = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	reStyle     = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	reSVG       = regexp.MustCompile(`(?is)<svg\b[^>]*>.*?</svg>`)
	reComment   = regexp.MustCompile(`(?s)<!--.*?-->`)
	reMultiWS   = regexp.MustCompile(`[ \t]+`)
	reBlankLine = regexp.MustCompile(`\n{3,}`)
)

// compactText runs the cheap-and-portable rules. Inputs may be HTML
// (rare — most extract paths return text), text with leftover
// boilerplate, or pure text; the rules are idempotent on plain text.
func compactText(in string) string {
	if in == "" {
		return in
	}
	out := in
	out = reScript.ReplaceAllString(out, "")
	out = reStyle.ReplaceAllString(out, "")
	out = reSVG.ReplaceAllString(out, "")
	out = reComment.ReplaceAllString(out, "")

	// Dedupe consecutive identical non-empty lines (boilerplate
	// like "Continue" "Continue" "Continue" in nav menus).
	lines := strings.Split(out, "\n")
	deduped := make([]string, 0, len(lines))
	var prev string
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" {
			deduped = append(deduped, ln)
			prev = ""
			continue
		}
		if trim == prev {
			continue
		}
		deduped = append(deduped, ln)
		prev = trim
	}
	out = strings.Join(deduped, "\n")

	out = reMultiWS.ReplaceAllString(out, " ")
	out = reBlankLine.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out)
}
