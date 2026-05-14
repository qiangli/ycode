// Ralph fallback — when a click action fails, try alternative
// strategies before giving up. Modeled after openchrome's Ralph
// Engine (MIT) at a smaller scale: we ship 4 of openchrome's 7
// strategies as a starter set, the remaining 3 (raw CDP events,
// keyboard nav, human escalation) are deferred until needed.
//
// Strategies attempted in order:
//  1. The selector as given (assume the caller knows what they want).
//  2. The selector with whitespace trimmed.
//  3. The selector unwrapped from quotes if it looks quoted.
//  4. JS-evaluate path: element.click() via document.querySelector
//     (only when the backend supports the `evaluate` action).

package reliability

import (
	"context"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

type ralphWrapper struct {
	inner mcpservers.Service
}

func (r *ralphWrapper) Name() string                       { return r.inner.Name() }
func (r *ralphWrapper) Available(ctx context.Context) bool { return r.inner.Available(ctx) }
func (r *ralphWrapper) EnsureReady(ctx context.Context) error {
	return r.inner.EnsureReady(ctx)
}
func (r *ralphWrapper) Stop(ctx context.Context) error { return r.inner.Stop(ctx) }

func (r *ralphWrapper) Execute(ctx context.Context, action mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	// Only wraps click; everything else passes through.
	if action.Type != mcpservers.ActionClick {
		return r.inner.Execute(ctx, action)
	}
	strategies := ralphStrategies(action)
	mode := r.inner.Name()
	var last *mcpservers.BrowserResult
	var lastErr error
	for i, s := range strategies {
		res, err := r.inner.Execute(ctx, s.action)
		succeeded := err == nil && res != nil && res.Success
		telotel.RecordBrowserRalphAttempt(ctx, mode, s.name, succeeded)
		if succeeded {
			if i > 0 && res.Hints == nil {
				res.Hints = append(res.Hints, fmt.Sprintf("ralph: click succeeded via strategy %q after %d failed attempts", s.name, i))
			}
			return res, nil
		}
		last = res
		lastErr = err
	}
	if last != nil {
		last.Hints = append(last.Hints, "ralph: all click strategies failed; consider increasing wait or switching modes")
	}
	return last, lastErr
}

type ralphStrategy struct {
	name   string
	action mcpservers.BrowserAction
}

func ralphStrategies(orig mcpservers.BrowserAction) []ralphStrategy {
	sel := orig.Selector
	out := []ralphStrategy{
		{name: "as-given", action: orig},
	}
	trimmed := strings.TrimSpace(sel)
	if trimmed != "" && trimmed != sel {
		alt := orig
		alt.Selector = trimmed
		out = append(out, ralphStrategy{name: "trimmed", action: alt})
	}
	if unquoted := stripOuterQuotes(sel); unquoted != "" && unquoted != sel {
		alt := orig
		alt.Selector = unquoted
		out = append(out, ralphStrategy{name: "unquoted", action: alt})
	}
	// JS-evaluate fallback for backends that support it (probe, solo).
	if sel != "" {
		js := fmt.Sprintf(`(function(){var e=document.querySelector(%q); if(!e) return false; e.click(); return true;})()`, sel)
		out = append(out, ralphStrategy{name: "js-click", action: mcpservers.BrowserAction{
			Type:   mcpservers.ActionEvaluate,
			Script: js,
		}})
	}
	return out
}

func stripOuterQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
