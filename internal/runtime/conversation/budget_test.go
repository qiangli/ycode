package conversation

import "testing"

func TestNewIterationBudget(t *testing.T) {
	b := NewIterationBudget(10)
	if b.Total != 10 {
		t.Errorf("Total = %d, want 10", b.Total)
	}
	if b.Used != 0 {
		t.Errorf("Used = %d, want 0", b.Used)
	}
	if b.GraceUsed {
		t.Error("GraceUsed should be false initially")
	}
}

func TestNewIterationBudget_ZeroDefault(t *testing.T) {
	b := NewIterationBudget(0)
	if b.Total != 1 {
		t.Errorf("Total = %d, want 1 (default for zero)", b.Total)
	}
}

func TestIterationBudget_NormalConsumption(t *testing.T) {
	b := NewIterationBudget(3)

	for i := 0; i < 3; i++ {
		if !b.Consume() {
			t.Errorf("Consume() returned false on iteration %d, expected true", i+1)
		}
	}
	if b.Remaining() != 0 {
		t.Errorf("Remaining() = %d, want 0", b.Remaining())
	}
	if b.IsExhausted() {
		t.Error("IsExhausted() should be false — grace call still available")
	}
}

func TestIterationBudget_GraceCall(t *testing.T) {
	b := NewIterationBudget(2)

	// Use both normal iterations.
	b.Consume()
	b.Consume()

	if b.IsGrace() {
		t.Error("IsGrace() should be false before grace call")
	}

	// Grace call.
	if !b.Consume() {
		t.Error("Consume() should allow the grace call")
	}
	if !b.IsGrace() {
		t.Error("IsGrace() should be true after grace call")
	}
	if b.Remaining() != 0 {
		t.Errorf("Remaining() = %d, want 0", b.Remaining())
	}
}

func TestIterationBudget_ExhaustedAfterGrace(t *testing.T) {
	b := NewIterationBudget(2)

	b.Consume() // 1
	b.Consume() // 2
	b.Consume() // grace

	if !b.IsExhausted() {
		t.Error("IsExhausted() should be true after grace call consumed")
	}

	// Further calls should be rejected.
	if b.Consume() {
		t.Error("Consume() should return false after exhaustion")
	}
}

func TestIterationBudget_Remaining(t *testing.T) {
	b := NewIterationBudget(5)

	if r := b.Remaining(); r != 5 {
		t.Errorf("Remaining() = %d, want 5", r)
	}
	b.Consume()
	b.Consume()
	if r := b.Remaining(); r != 3 {
		t.Errorf("Remaining() = %d, want 3", r)
	}
}

func TestIterationBudget_GraceMessage(t *testing.T) {
	b := NewIterationBudget(1)
	msg := b.GraceMessage()
	if msg == "" {
		t.Error("GraceMessage() should not be empty")
	}
}

func TestIterationBudget_SingleIteration(t *testing.T) {
	b := NewIterationBudget(1)

	if !b.Consume() {
		t.Error("first Consume() should succeed")
	}
	if !b.Consume() {
		t.Error("grace Consume() should succeed")
	}
	if b.Consume() {
		t.Error("third Consume() should fail")
	}
	if !b.IsExhausted() {
		t.Error("should be exhausted")
	}
}
