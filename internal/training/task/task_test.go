package task

import "testing"

func TestGSM8K_GetExample(t *testing.T) {
	g := NewGSM8K()
	if g.Len() != 5 {
		t.Fatalf("expected 5 examples, got %d", g.Len())
	}
	ex, err := g.GetExample(0)
	if err != nil {
		t.Fatal(err)
	}
	if ex.ID != "gsm8k-001" {
		t.Errorf("expected ID gsm8k-001, got %s", ex.ID)
	}
	if _, err := g.GetExample(-1); err == nil {
		t.Error("expected error for negative index")
	}
	if _, err := g.GetExample(5); err == nil {
		t.Error("expected error for out-of-range index")
	}
}

func TestGSM8K_Evaluate(t *testing.T) {
	g := NewGSM8K()
	ex, _ := g.GetExample(0) // expected "18"

	// Correct answer.
	score, err := g.Evaluate(ex, "The answer is 18.")
	if err != nil {
		t.Fatal(err)
	}
	if score != 1.0 {
		t.Errorf("expected score 1.0, got %f", score)
	}

	// Incorrect answer.
	score, err = g.Evaluate(ex, "The answer is 20.")
	if err != nil {
		t.Fatal(err)
	}
	if score != 0.0 {
		t.Errorf("expected score 0.0, got %f", score)
	}

	// No number in completion.
	score, err = g.Evaluate(ex, "I don't know the answer.")
	if err != nil {
		t.Fatal(err)
	}
	if score != 0.0 {
		t.Errorf("expected score 0.0, got %f", score)
	}
}

func TestTerminalTask_Basics(t *testing.T) {
	tt := NewTerminalTask()
	if tt.Name() != "terminal" {
		t.Errorf("expected name 'terminal', got %s", tt.Name())
	}
	if tt.Len() != 3 {
		t.Fatalf("expected 3 examples, got %d", tt.Len())
	}
	ex, err := tt.GetExample(0)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Metadata["verify_path"] != "~/greeting.txt" {
		t.Errorf("unexpected metadata: %v", ex.Metadata)
	}

	score, err := tt.Evaluate(ex, "I created the file with: Hello from ycode")
	if err != nil {
		t.Fatal(err)
	}
	if score != 1.0 {
		t.Errorf("expected score 1.0, got %f", score)
	}

	score, err = tt.Evaluate(ex, "Done.")
	if err != nil {
		t.Fatal(err)
	}
	if score != 0.0 {
		t.Errorf("expected score 0.0, got %f", score)
	}
}

func TestMixture_Len(t *testing.T) {
	g := NewGSM8K()
	tt := NewTerminalTask()
	m, err := NewMixture([]Task{g, tt}, []float64{1.0, 1.0})
	if err != nil {
		t.Fatal(err)
	}
	expected := g.Len() + tt.Len()
	if m.Len() != expected {
		t.Errorf("expected Len %d, got %d", expected, m.Len())
	}
}

func TestMixture_GetExample(t *testing.T) {
	g := NewGSM8K()
	tt := NewTerminalTask()
	m, err := NewMixture([]Task{g, tt}, []float64{1.0, 1.0})
	if err != nil {
		t.Fatal(err)
	}

	// First task's examples.
	ex, err := m.GetExample(0)
	if err != nil {
		t.Fatal(err)
	}
	if ex.ID != "gsm8k-001" {
		t.Errorf("expected gsm8k-001, got %s", ex.ID)
	}

	// Second task's examples (offset by g.Len()).
	ex, err = m.GetExample(g.Len())
	if err != nil {
		t.Fatal(err)
	}
	if ex.ID != "term-001" {
		t.Errorf("expected term-001, got %s", ex.ID)
	}

	// Out of range.
	if _, err := m.GetExample(m.Len()); err == nil {
		t.Error("expected error for out-of-range index")
	}
}

func TestMixture_NewErrors(t *testing.T) {
	g := NewGSM8K()
	if _, err := NewMixture([]Task{g}, []float64{1.0, 2.0}); err == nil {
		t.Error("expected error for mismatched lengths")
	}
	if _, err := NewMixture([]Task{g}, []float64{-1.0}); err == nil {
		t.Error("expected error for negative weight")
	}
}
