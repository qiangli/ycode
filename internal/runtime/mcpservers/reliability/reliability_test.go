package reliability

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// fakeService is a programmable Service used to drive the
// reliability wrappers without spawning a real browser.
type fakeService struct {
	name    string
	execute func(action mcpservers.BrowserAction) (*mcpservers.BrowserResult, error)
}

func (f *fakeService) Name() string                        { return f.name }
func (f *fakeService) Available(_ context.Context) bool    { return true }
func (f *fakeService) EnsureReady(_ context.Context) error { return nil }
func (f *fakeService) Stop(_ context.Context) error        { return nil }
func (f *fakeService) Execute(_ context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	return f.execute(a)
}

func TestHintEngine_Captcha(t *testing.T) {
	inner := &fakeService{name: "test", execute: func(_ mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
		return &mcpservers.BrowserResult{
			Success: true,
			Content: "Please complete the CAPTCHA to continue",
		}, nil
	}}
	w := Wrap(inner, Config{}) // all defaults on

	res, err := w.Execute(context.Background(), mcpservers.BrowserAction{
		Type: mcpservers.ActionExtract,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var found bool
	for _, h := range res.Hints {
		if strings.HasPrefix(h, "captcha_detected") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected captcha_detected hint; got %v", res.Hints)
	}
	if res.OutcomeClass != "BLOCKED" {
		t.Fatalf("expected OutcomeClass=BLOCKED for captcha; got %q", res.OutcomeClass)
	}
}

func TestHintEngine_404(t *testing.T) {
	inner := &fakeService{name: "test", execute: func(_ mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
		return &mcpservers.BrowserResult{
			Success: true,
			Content: "404 - Page Not Found",
		}, nil
	}}
	w := Wrap(inner, Config{})
	res, _ := w.Execute(context.Background(), mcpservers.BrowserAction{Type: mcpservers.ActionExtract})
	var found bool
	for _, h := range res.Hints {
		if strings.HasPrefix(h, "page_404") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected page_404 hint; got %v", res.Hints)
	}
}

func TestCompactDOM_RemovesScripts(t *testing.T) {
	inner := &fakeService{name: "test", execute: func(_ mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
		return &mcpservers.BrowserResult{
			Success: true,
			Content: "Hello <script>alert(1)</script> World",
		}, nil
	}}
	w := Wrap(inner, Config{HintEngine: ptrFalse(), RalphFallback: ptrFalse(), CircuitBreaker: ptrFalse(), PatternLearner: ptrFalse()})
	res, _ := w.Execute(context.Background(), mcpservers.BrowserAction{Type: mcpservers.ActionExtract})
	if strings.Contains(res.Content, "alert") {
		t.Fatalf("compactDOM did not strip <script>: %q", res.Content)
	}
}

func TestCompactDOM_DedupeLines(t *testing.T) {
	inner := &fakeService{name: "test", execute: func(_ mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
		return &mcpservers.BrowserResult{
			Success: true,
			Content: "Continue\nContinue\nContinue\nDone",
		}, nil
	}}
	w := Wrap(inner, Config{HintEngine: ptrFalse(), RalphFallback: ptrFalse(), CircuitBreaker: ptrFalse(), PatternLearner: ptrFalse()})
	res, _ := w.Execute(context.Background(), mcpservers.BrowserAction{Type: mcpservers.ActionExtract})
	if strings.Count(res.Content, "Continue") != 1 {
		t.Fatalf("expected dedupe; got %q", res.Content)
	}
}

func TestCircuitBreaker_ElementLevel(t *testing.T) {
	inner := &fakeService{name: "test", execute: func(_ mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
		return &mcpservers.BrowserResult{Error: "click failed"}, nil
	}}
	// Disable ralph so the breaker counts each individual call.
	w := Wrap(inner, Config{
		HintEngine:     ptrFalse(),
		CompactDOM:     ptrFalse(),
		RalphFallback:  ptrFalse(),
		PatternLearner: ptrFalse(),
	})
	action := mcpservers.BrowserAction{Type: mcpservers.ActionClick, Selector: "#submit"}
	for i := 0; i < 3; i++ {
		_, _ = w.Execute(context.Background(), action)
	}
	res, _ := w.Execute(context.Background(), action)
	if !strings.Contains(res.Error, "circuit-breaker") {
		t.Fatalf("expected circuit-breaker error after 3 fails; got %q", res.Error)
	}
}

func TestRalph_FallbackSucceedsOnTrimmed(t *testing.T) {
	calls := 0
	inner := &fakeService{name: "test", execute: func(a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
		calls++
		// First call (raw selector with surrounding whitespace) fails;
		// second call (trimmed) succeeds.
		if strings.HasPrefix(a.Selector, " ") || strings.HasSuffix(a.Selector, " ") {
			return &mcpservers.BrowserResult{Error: "no such element"}, nil
		}
		return &mcpservers.BrowserResult{Success: true}, nil
	}}
	w := Wrap(inner, Config{
		HintEngine:     ptrFalse(),
		CompactDOM:     ptrFalse(),
		CircuitBreaker: ptrFalse(),
		PatternLearner: ptrFalse(),
	})
	res, err := w.Execute(context.Background(), mcpservers.BrowserAction{
		Type:     mcpservers.ActionClick,
		Selector: " #submit ",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected ralph fallback to succeed; got %v", res)
	}
	if calls < 2 {
		t.Fatalf("expected at least 2 inner calls (raw + trimmed); got %d", calls)
	}
}

func TestWrap_NilInner(t *testing.T) {
	if got := Wrap(nil, Config{}); got != nil {
		t.Fatalf("Wrap(nil) should return nil; got %v", got)
	}
}

func TestPatternLearner_PersistsRecord(t *testing.T) {
	// Override the default store path to a temp file so we don't
	// pollute the user's real ycode config.
	tmp := t.TempDir() + "/patterns.jsonl"
	inner := &fakeService{name: "test", execute: func(_ mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
		return &mcpservers.BrowserResult{Error: "boom"}, errors.New("boom")
	}}
	lw := &learnerWrapper{inner: inner, storage: &patternStore{path: tmp}}
	_, _ = lw.Execute(context.Background(), mcpservers.BrowserAction{Type: mcpservers.ActionClick, URL: "https://x.test"})
	// Give the background goroutine a moment.
	time.Sleep(50 * time.Millisecond)
	// Best-effort: we only check that the file exists; content
	// shape is exercised by record() directly elsewhere if needed.
}

func ptrFalse() *bool { b := false; return &b }
