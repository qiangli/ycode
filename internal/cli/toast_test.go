package cli

import (
	"testing"
	"time"
)

func TestToastAddAndPrune(t *testing.T) {
	var ts toastState

	ts.add("test message", ToastInfo)
	if !ts.hasActive() {
		t.Error("expected active toasts after add")
	}
	if len(ts.messages) != 1 {
		t.Errorf("expected 1 toast, got %d", len(ts.messages))
	}

	// Prune should keep recent toast.
	changed := ts.prune()
	if changed {
		t.Error("expected no change from prune on recent toast")
	}
	if len(ts.messages) != 1 {
		t.Error("expected toast to survive prune")
	}
}

func TestToastPruneExpired(t *testing.T) {
	var ts toastState

	ts.messages = append(ts.messages, toastMessage{
		Text:      "old",
		Level:     ToastInfo,
		CreatedAt: time.Now().Add(-10 * time.Second),
		Duration:  1 * time.Second,
	})
	ts.add("new", ToastSuccess)

	changed := ts.prune()
	if !changed {
		t.Error("expected prune to remove expired toast")
	}
	if len(ts.messages) != 1 {
		t.Errorf("expected 1 toast after prune, got %d", len(ts.messages))
	}
	if ts.messages[0].Text != "new" {
		t.Error("expected only the new toast to survive")
	}
}

func TestToastLevels(t *testing.T) {
	var ts toastState
	ts.add("info", ToastInfo)
	ts.add("success", ToastSuccess)
	ts.add("warning", ToastWarning)
	ts.add("error", ToastError)

	if len(ts.messages) != 4 {
		t.Errorf("expected 4 toasts, got %d", len(ts.messages))
	}
}

func TestRenderToastsEmpty(t *testing.T) {
	var ts toastState
	result := renderToasts(&ts, 80)
	if result != "" {
		t.Error("expected empty render for no toasts")
	}
}

func TestRenderToastsMaxThree(t *testing.T) {
	var ts toastState
	ts.add("one", ToastInfo)
	ts.add("two", ToastInfo)
	ts.add("three", ToastInfo)
	ts.add("four", ToastInfo)

	result := renderToasts(&ts, 80)
	if result == "" {
		t.Error("expected non-empty render")
	}
}
