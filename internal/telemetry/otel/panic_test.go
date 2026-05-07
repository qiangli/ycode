package otel

import (
	"context"
	"strings"
	"testing"
)

func TestRecordPanic_NilReturnsNil(t *testing.T) {
	if err := RecordPanic(context.Background(), "test", "x", nil); err != nil {
		t.Errorf("RecordPanic(nil) = %v, want nil", err)
	}
}

func TestRecordPanic_RealPanicProducesError(t *testing.T) {
	err := RecordPanic(context.Background(), "test.invoke", "boom-tool", "kaboom")
	if err == nil {
		t.Fatal("RecordPanic should return non-nil for non-nil recovered")
	}
	msg := err.Error()
	for _, want := range []string{"test.invoke", "boom-tool", "kaboom"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
}

// TestSafeInvoke_RecoversPanic exercises the integration shape used by
// tools/registry, taskqueue, and spawner: a deferred recover() that
// converts a panic into an error via RecordPanic.
func TestSafeInvoke_RecoversPanic(t *testing.T) {
	got := safeRun(context.Background(), func() {
		panic("inner kaboom")
	})
	if got == nil {
		t.Fatal("expected error from panicking call, got nil")
	}
	if !strings.Contains(got.Error(), "inner kaboom") {
		t.Errorf("error %q missing panic message", got)
	}
}

// safeRun mirrors the pattern that real call sites use.
func safeRun(ctx context.Context, fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = RecordPanic(ctx, "test.safeRun", "anonymous", r)
		}
	}()
	fn()
	return nil
}
