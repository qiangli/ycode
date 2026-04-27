package mesh

import (
	"testing"
)

func TestSafetyGuardCanFix(t *testing.T) {
	sg := NewSafetyGuard(3, 2)

	ok, reason := sg.CanFix("r1")
	if !ok {
		t.Fatalf("should allow first fix: %s", reason)
	}

	sg.RecordFix("r1")
	sg.RecordFix("r1")

	// Per-report limit reached.
	ok, reason = sg.CanFix("r1")
	if ok {
		t.Fatal("should deny fix after per-report limit reached")
	}
	if reason == "" {
		t.Fatal("reason should be non-empty")
	}

	// Different report should still work.
	ok, _ = sg.CanFix("r2")
	if !ok {
		t.Fatal("different report should be allowed")
	}
}

func TestSafetyGuardHourlyBudget(t *testing.T) {
	sg := NewSafetyGuard(2, 10)

	sg.RecordFix("a")
	sg.RecordFix("b")

	ok, reason := sg.CanFix("c")
	if ok {
		t.Fatal("should deny fix after hourly budget exhausted")
	}
	if reason == "" {
		t.Fatal("reason should be non-empty")
	}
}

func TestSafetyGuardReset(t *testing.T) {
	sg := NewSafetyGuard(1, 1)
	sg.RecordFix("r1")

	ok, _ := sg.CanFix("r1")
	if ok {
		t.Fatal("should deny after recording")
	}

	sg.Reset()

	ok, _ = sg.CanFix("r1")
	if !ok {
		t.Fatal("should allow after reset")
	}
}

func TestSafetyGuardDefaults(t *testing.T) {
	sg := NewSafetyGuard(0, 0)
	if sg.maxFixesPerHour != 5 {
		t.Fatalf("expected default maxFixesPerHour 5, got %d", sg.maxFixesPerHour)
	}
	if sg.maxAttemptsPerReport != 2 {
		t.Fatalf("expected default maxAttemptsPerReport 2, got %d", sg.maxAttemptsPerReport)
	}
}
