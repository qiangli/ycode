package reliability

import (
	"context"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// TestRalphErrorEnumeratesAttempts confirms that when every strategy
// fails the final hint lists each strategy + its specific failure.
func TestRalphErrorEnumeratesAttempts(t *testing.T) {
	inner := &fakeService{
		name: "live",
		execute: func(a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
			return &mcpservers.BrowserResult{Error: "element not found: " + a.Selector}, nil
		},
	}
	r := &ralphWrapper{inner: inner}
	res, err := r.Execute(context.Background(), mcpservers.BrowserAction{
		Type:     mcpservers.ActionClick,
		Selector: "  .does-not-exist  ",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(res.Hints) == 0 {
		t.Fatalf("expected enumerated-failure hint; got no hints")
	}
	last := res.Hints[len(res.Hints)-1]
	if !strings.Contains(last, "as-given") || !strings.Contains(last, "trimmed") {
		t.Fatalf("hint missing strategy names: %q", last)
	}
	if !strings.Contains(last, "click strategies failed") {
		t.Fatalf("hint missing 'click strategies failed' phrase: %q", last)
	}
}

// TestRalphSuccessByJSClickAnnotates verifies that when the original
// selector fails but the js-click fallback succeeds, the result gets a
// hint noting which strategy won.
func TestRalphSuccessByJSClickAnnotates(t *testing.T) {
	inner := &fakeService{
		name: "probe",
		execute: func(a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
			if a.Type == mcpservers.ActionEvaluate {
				return &mcpservers.BrowserResult{Success: true, Data: "true"}, nil
			}
			return &mcpservers.BrowserResult{Error: "fail"}, nil
		},
	}
	r := &ralphWrapper{inner: inner}
	res, err := r.Execute(context.Background(), mcpservers.BrowserAction{
		Type:     mcpservers.ActionClick,
		Selector: ".x",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success")
	}
	found := false
	for _, h := range res.Hints {
		if strings.Contains(h, "js-click") || strings.Contains(h, "succeeded via strategy") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("hint missing strategy-name annotation: %+v", res.Hints)
	}
}

// TestRalphElementIDPassthrough exercises the regression where a
// click driven by element_id alone (no selector, no match_text)
// short-circuited with "all 0 click strategies failed — " — the
// trailing empty-tail message that motivated the fix. The
// pass-through strategy now reaches the backend, which resolves the
// element_id natively in live mode.
func TestRalphElementIDPassthrough(t *testing.T) {
	var sawElementID int
	inner := &fakeService{
		name: "live",
		execute: func(a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
			sawElementID = a.ElementID
			return &mcpservers.BrowserResult{Success: true}, nil
		},
	}
	r := &ralphWrapper{inner: inner}
	res, err := r.Execute(context.Background(), mcpservers.BrowserAction{
		Type:      mcpservers.ActionClick,
		ElementID: 25,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected element_id pass-through to succeed: %+v", res)
	}
	if sawElementID != 25 {
		t.Fatalf("backend should have received ElementID=25, got %d", sawElementID)
	}
}

// TestRalphNoHintRequestIsDirective pins the message shape when the
// caller passes no selector, match_text, OR element_id — the case
// where the old code emitted "all 0 click strategies failed —" with
// nothing past the dash and no actionable result error.
func TestRalphNoHintRequestIsDirective(t *testing.T) {
	inner := &fakeService{
		name: "live",
		execute: func(a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
			t.Fatalf("backend should not be called when no click hint is provided; got %+v", a)
			return nil, nil
		},
	}
	r := &ralphWrapper{inner: inner}
	res, err := r.Execute(context.Background(), mcpservers.BrowserAction{
		Type: mcpservers.ActionClick,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Success {
		t.Fatalf("expected failure when no click hint provided")
	}
	if res.Error == "" || !strings.Contains(res.Error, "selector") || !strings.Contains(res.Error, "element_id") {
		t.Fatalf("error should name the missing inputs; got %q", res.Error)
	}
	if len(res.Hints) == 0 {
		t.Fatalf("expected directive hint, got none")
	}
	last := res.Hints[len(res.Hints)-1]
	if !strings.Contains(last, "0 click strategies applied") {
		t.Fatalf("hint should distinguish 'no strategies applied' from 'all failed'; got %q", last)
	}
}

// TestRalphTextClickStrategy verifies the new MatchText-based
// strategies. With js-text-click returning "false" (no DOM match) the
// final extract-by-text + click-by-id strategy should succeed.
func TestRalphTextClickStrategy(t *testing.T) {
	type ev struct {
		action    string
		text      string
		elementID int
	}
	var events []ev
	inner := &fakeService{
		name: "live",
		execute: func(a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
			events = append(events, ev{action: a.Type, text: a.MatchText, elementID: a.ElementID})
			switch a.Type {
			case mcpservers.ActionEvaluate:
				return &mcpservers.BrowserResult{Success: true, Data: "false"}, nil
			case mcpservers.ActionExtract:
				return &mcpservers.BrowserResult{Success: true, Total: 1, Elements: "[1] <button>Copy</button>"}, nil
			case mcpservers.ActionClick:
				if a.ElementID == 1 {
					return &mcpservers.BrowserResult{Success: true}, nil
				}
				return &mcpservers.BrowserResult{Error: "no selector match"}, nil
			}
			return &mcpservers.BrowserResult{Error: "unexpected action"}, nil
		},
	}
	r := &ralphWrapper{inner: inner}
	res, err := r.Execute(context.Background(), mcpservers.BrowserAction{
		Type:      mcpservers.ActionClick,
		MatchText: "Copy",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected click to ultimately succeed via extract-by-text: %+v", res)
	}
	sawExtract := false
	sawClickByID := false
	for _, e := range events {
		if e.action == mcpservers.ActionExtract && e.text == "Copy" {
			sawExtract = true
		}
		if e.action == mcpservers.ActionClick && e.elementID == 1 {
			sawClickByID = true
		}
	}
	if !sawExtract {
		t.Fatalf("expected extract-by-text with MatchText=Copy; events=%+v", events)
	}
	if !sawClickByID {
		t.Fatalf("expected click(element_id=1) to follow extract; events=%+v", events)
	}
}
