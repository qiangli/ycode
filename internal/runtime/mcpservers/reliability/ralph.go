// Ralph fallback — when a click action fails, try alternative
// strategies before giving up. Modeled after openchrome's Ralph
// Engine (MIT) at a smaller scale.
//
// Strategies attempted in order (each is skipped when its
// preconditions aren't met):
//  1. element-id-passthrough — when ElementID > 0 and no selector/
//                       match_text was given, pass the original action
//                       straight to the backend (live mode resolves
//                       element_id natively; probe surfaces a clear
//                       backend error instead of "0 strategies").
//  2. as-given        — selector unchanged.
//  3. trimmed         — whitespace stripped.
//  4. unquoted        — outer matching quotes removed.
//  5. js-click        — document.querySelector + element.click() via
//                       the `evaluate` action (probe/solo).
//  6. js-text-click   — when MatchText is set, walk visible text and
//                       click the first match (probe/solo via eval,
//                       live via the extension's matching path).
//  7. extract-click-by-text — when MatchText is set, run extract with
//                       goal=MatchText and click the first returned
//                       element_id. Works in every mode.
//
// On total failure the wrapper enumerates each strategy + reason in
// the hint so the caller can see exactly which paths were tried.
// When zero strategies even applied (e.g. caller passed no selector,
// match_text, OR element_id), the hint says so explicitly rather
// than emitting a misleading "all 0 strategies failed —".

package reliability

import (
	"context"
	"encoding/json"
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
	strategies := ralphStrategies(r.inner, action)
	mode := r.inner.Name()
	var last *mcpservers.BrowserResult
	var lastErr error
	type attempt struct {
		name string
		fail string
	}
	var failed []attempt
	for i, s := range strategies {
		res, err := s.run(ctx)
		succeeded := err == nil && res != nil && res.Success
		telotel.RecordBrowserRalphAttempt(ctx, mode, s.name, succeeded)
		if succeeded {
			if i > 0 {
				res.Hints = append(res.Hints, fmt.Sprintf("ralph: click succeeded via strategy %q after %d failed attempts", s.name, i))
			}
			return res, nil
		}
		failed = append(failed, attempt{name: s.name, fail: reasonOf(res, err)})
		last = res
		lastErr = err
	}
	telotel.RecordBrowserRalphExhausted(ctx, mode, len(failed))
	if last == nil {
		last = &mcpservers.BrowserResult{}
	}
	// Distinguish "tried N strategies, all failed" from "the caller
	// passed no clickable hint at all". The second case is the harder-
	// to-debug one — emit a directive error instead of an enumeration
	// that reads as a silent no-op.
	var hint string
	if len(failed) == 0 {
		last.Error = "click: provide one of `selector`, `match_text`, or `element_id` (from a prior browser_extract result)"
		hint = "ralph: 0 click strategies applied — " + last.Error
	} else {
		var parts []string
		for _, a := range failed {
			parts = append(parts, fmt.Sprintf("%s: %s", a.name, a.fail))
		}
		hint = fmt.Sprintf("ralph: all %d click strategies failed — %s", len(failed), strings.Join(parts, "; "))
	}
	last.Hints = append(last.Hints, hint)
	return last, lastErr
}

func reasonOf(r *mcpservers.BrowserResult, err error) string {
	switch {
	case err != nil:
		return err.Error()
	case r != nil && r.Error != "":
		return r.Error
	case r == nil:
		return "no result"
	default:
		return "no match"
	}
}

type ralphStrategy struct {
	name string
	run  func(ctx context.Context) (*mcpservers.BrowserResult, error)
}

func ralphStrategies(inner mcpservers.Service, orig mcpservers.BrowserAction) []ralphStrategy {
	sel := orig.Selector
	out := []ralphStrategy{}

	// element-id-passthrough — when the caller drove the click off an
	// element_id returned by a prior browser_extract/browser_navigate,
	// the original action already carries everything the backend needs.
	// In live mode the Chrome extension resolves element_id via the
	// same enumeration extract uses (see live/extension/background.js
	// resolveTarget). In probe mode the backend currently rejects
	// element_id-only clicks with a clear error — better than the old
	// "all 0 strategies failed —" empty-tail message.
	if orig.ElementID > 0 && sel == "" && orig.MatchText == "" && orig.Goal == "" {
		out = append(out, ralphStrategy{
			name: "element-id-passthrough",
			run:  func(ctx context.Context) (*mcpservers.BrowserResult, error) { return inner.Execute(ctx, orig) },
		})
	}

	if sel != "" {
		out = append(out, ralphStrategy{
			name: "as-given",
			run:  func(ctx context.Context) (*mcpservers.BrowserResult, error) { return inner.Execute(ctx, orig) },
		})
		trimmed := strings.TrimSpace(sel)
		if trimmed != "" && trimmed != sel {
			alt := orig
			alt.Selector = trimmed
			out = append(out, ralphStrategy{
				name: "trimmed",
				run:  func(ctx context.Context) (*mcpservers.BrowserResult, error) { return inner.Execute(ctx, alt) },
			})
		}
		if unquoted := stripOuterQuotes(sel); unquoted != "" && unquoted != sel {
			alt := orig
			alt.Selector = unquoted
			out = append(out, ralphStrategy{
				name: "unquoted",
				run:  func(ctx context.Context) (*mcpservers.BrowserResult, error) { return inner.Execute(ctx, alt) },
			})
		}
		js := fmt.Sprintf(`(function(){var e=document.querySelector(%q); if(!e) return false; e.click(); return true;})()`, sel)
		jsAct := mcpservers.BrowserAction{Type: mcpservers.ActionEvaluate, Script: js}
		out = append(out, ralphStrategy{
			name: "js-click",
			run: func(ctx context.Context) (*mcpservers.BrowserResult, error) {
				res, err := inner.Execute(ctx, jsAct)
				// `data` is the stringified return value: "true"
				// when the element was found and clicked, "false"
				// otherwise. Without this check ralph would
				// short-circuit on a no-op evaluate.
				if err == nil && res != nil && res.Error == "" {
					if !strings.Contains(strings.ToLower(res.Data), "true") {
						res.Success = false
						res.Error = "js-click: selector returned null"
					}
				}
				return res, err
			},
		})
	}

	// Text-based fallbacks. Either the caller passed MatchText
	// explicitly, or the original click was issued with element_id == 0
	// AND no selector (text-only). We use Goal as a backup since the
	// retrospective shows callers occasionally pass the button label
	// there expecting it to do something useful.
	text := orig.MatchText
	if text == "" {
		text = orig.Goal
	}
	if text != "" {
		// js-text-click: walks the DOM for elements whose visible text
		// includes the requested substring, case-insensitive, then
		// clicks the first match. Works through the existing evaluate
		// surface — present in all three modes.
		jsText := fmt.Sprintf(`(function(){
  var t = %s.toLowerCase();
  var nodes = document.querySelectorAll("a, button, input[type=button], input[type=submit], [role='button'], [role='link']");
  for (var i=0; i<nodes.length; i++) {
    var n = nodes[i];
    var v = (n.innerText || n.value || n.getAttribute("aria-label") || "").trim().toLowerCase();
    if (v.indexOf(t) >= 0) { n.click(); return true; }
  }
  return false;
})()`, jsStringLit(text))
		jsAct := mcpservers.BrowserAction{Type: mcpservers.ActionEvaluate, Script: jsText}
		out = append(out, ralphStrategy{
			name: "js-text-click",
			run: func(ctx context.Context) (*mcpservers.BrowserResult, error) {
				res, err := inner.Execute(ctx, jsAct)
				if err == nil && res != nil && res.Error == "" {
					// `data` is the stringified return value: "true"
					// when we found and clicked, "false" otherwise.
					if !strings.Contains(strings.ToLower(res.Data), "true") {
						res.Success = false
						res.Error = "js-text-click: no element matched text"
					}
				}
				return res, err
			},
		})

		// extract-click-by-text: do an extract scoped by MatchText,
		// then click the first matching element_id. Works in every
		// mode (including live, where evaluate may be unavailable on
		// older extensions).
		extractAct := mcpservers.BrowserAction{
			Type:      mcpservers.ActionExtract,
			MatchText: text,
			Scope:     orig.Scope,
			Limit:     1,
		}
		out = append(out, ralphStrategy{
			name: "extract-click-by-text",
			run: func(ctx context.Context) (*mcpservers.BrowserResult, error) {
				res, err := inner.Execute(ctx, extractAct)
				if err != nil {
					return res, err
				}
				if res == nil || !res.Success || res.Total == 0 {
					return &mcpservers.BrowserResult{Error: "extract-click-by-text: no element matched"}, nil
				}
				clickAct := mcpservers.BrowserAction{Type: mcpservers.ActionClick, ElementID: 1, Scope: orig.Scope}
				return inner.Execute(ctx, clickAct)
			},
		})
	}

	return out
}

func stripOuterQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func jsStringLit(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
