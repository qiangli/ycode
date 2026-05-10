//go:build experimental

// Hint Engine — rule-based detection of common browser-automation
// failure modes (bot walls, repetition loops, blocking pages,
// CAPTCHAs). Each rule looks at the BrowserResult and may append a
// hint string the agent reads to decide next action.
//
// Ports openchrome's (MIT) Hint Engine design at a small scale (8
// starter rules); add more rules over time by appending to the
// hintRules slice. Each rule is a pure function — no shared state.

package reliability

import (
	"context"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

type hintEngineWrapper struct {
	inner mcpservers.Service
}

func (h *hintEngineWrapper) Name() string                       { return h.inner.Name() }
func (h *hintEngineWrapper) Available(ctx context.Context) bool { return h.inner.Available(ctx) }
func (h *hintEngineWrapper) EnsureReady(ctx context.Context) error {
	return h.inner.EnsureReady(ctx)
}
func (h *hintEngineWrapper) Stop(ctx context.Context) error { return h.inner.Stop(ctx) }

func (h *hintEngineWrapper) Execute(ctx context.Context, action mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	res, err := h.inner.Execute(ctx, action)
	if err != nil {
		return res, err
	}
	if res == nil {
		return res, nil
	}
	// Run every rule; collect hints. Also classify outcome (the
	// Outcome Classifier is a pseudo-rule that always fires).
	for _, rule := range hintRules {
		if h := rule(action, res); h != "" {
			res.Hints = append(res.Hints, h)
		}
	}
	res.OutcomeClass = classifyOutcome(action, res)
	return res, nil
}

// hintRule is a pure check returning an empty string when the rule
// does not match, or a one-line agent-facing hint when it does.
type hintRule func(action mcpservers.BrowserAction, res *mcpservers.BrowserResult) string

var hintRules = []hintRule{
	ruleCaptcha,
	ruleCloudflare,
	ruleRateLimit,
	ruleLoginWall,
	ruleNotFound,
	ruleEmptyContent,
	ruleServerError,
	ruleDeprecationBanner,
}

func contentLower(res *mcpservers.BrowserResult) string {
	return strings.ToLower(res.Content)
}

func ruleCaptcha(_ mcpservers.BrowserAction, res *mcpservers.BrowserResult) string {
	low := contentLower(res)
	if strings.Contains(low, "captcha") || strings.Contains(low, "i'm not a robot") ||
		strings.Contains(low, "verify you are human") || strings.Contains(low, "recaptcha") {
		return "captcha_detected: page is gated by a human-verification check; consider switching to `live` mode (user's real Chrome) or aborting"
	}
	return ""
}

func ruleCloudflare(_ mcpservers.BrowserAction, res *mcpservers.BrowserResult) string {
	low := contentLower(res)
	if strings.Contains(low, "checking your browser") || strings.Contains(low, "cloudflare") &&
		strings.Contains(low, "ray id") {
		return "cloudflare_challenge: Cloudflare interstitial detected; CDP fingerprint may be blocked, prefer `live` mode"
	}
	return ""
}

func ruleRateLimit(_ mcpservers.BrowserAction, res *mcpservers.BrowserResult) string {
	low := contentLower(res)
	if strings.Contains(low, "rate limit") || strings.Contains(low, "too many requests") ||
		strings.Contains(low, "429") {
		return "rate_limited: backend reported rate limiting; back off before retrying"
	}
	return ""
}

func ruleLoginWall(_ mcpservers.BrowserAction, res *mcpservers.BrowserResult) string {
	low := contentLower(res)
	if strings.Contains(low, "sign in") || strings.Contains(low, "log in") ||
		strings.Contains(low, "please log in") {
		// Heuristic: only flag if the page is small (login forms are usually short).
		if len(res.Content) < 3000 {
			return "login_wall: page appears to require authentication; if user wanted real session, switch to `live` mode"
		}
	}
	return ""
}

func ruleNotFound(_ mcpservers.BrowserAction, res *mcpservers.BrowserResult) string {
	low := contentLower(res)
	if strings.Contains(low, "404") && (strings.Contains(low, "not found") || strings.Contains(low, "page does not exist")) {
		return "page_404: target URL returned 404; do not retry the same URL"
	}
	return ""
}

func ruleEmptyContent(action mcpservers.BrowserAction, res *mcpservers.BrowserResult) string {
	if action.Type == mcpservers.ActionExtract && len(strings.TrimSpace(res.Content)) < 50 {
		return "empty_content: extraction returned almost no text; page may be JS-heavy — wait, scroll, or use `solo` headed mode"
	}
	return ""
}

func ruleServerError(_ mcpservers.BrowserAction, res *mcpservers.BrowserResult) string {
	low := contentLower(res)
	if strings.Contains(low, "500 internal server error") ||
		strings.Contains(low, "503 service unavailable") ||
		strings.Contains(low, "502 bad gateway") {
		return "server_error: upstream returned 5xx; do not retry immediately"
	}
	return ""
}

func ruleDeprecationBanner(_ mcpservers.BrowserAction, res *mcpservers.BrowserResult) string {
	low := contentLower(res)
	if strings.Contains(low, "this site is no longer maintained") ||
		strings.Contains(low, "this page has moved") {
		return "deprecated_page: site signals deprecation; look for a successor URL in the page"
	}
	return ""
}

// classifyOutcome maps the result + action into one of:
//
//	SUCCESS | SILENT_CLICK | WRONG_ELEMENT | AUTH_REDIRECT | BLOCKED
//
// Mirrors openchrome's Outcome Classifier semantics.
func classifyOutcome(action mcpservers.BrowserAction, res *mcpservers.BrowserResult) string {
	if res.Error != "" {
		return "BLOCKED"
	}
	if !res.Success {
		return "BLOCKED"
	}
	for _, h := range res.Hints {
		if strings.HasPrefix(h, "captcha_detected") ||
			strings.HasPrefix(h, "cloudflare_challenge") ||
			strings.HasPrefix(h, "rate_limited") {
			return "BLOCKED"
		}
		if strings.HasPrefix(h, "login_wall") {
			return "AUTH_REDIRECT"
		}
	}
	// SILENT_CLICK: click action succeeded but page didn't change.
	// Without before/after URL comparison we approximate by
	// checking for an empty content payload after click.
	if action.Type == mcpservers.ActionClick && len(strings.TrimSpace(res.Content)) == 0 && res.URL == "" {
		return "SILENT_CLICK"
	}
	return "SUCCESS"
}
