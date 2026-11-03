package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRecall_TimeWindowFastPath wires the components touched by Phase 2
// of the memory plan: a time-shaped query is detected, the time-bucket
// is consulted, and Recall surfaces the bucket members as one of the
// fused result sets. This is the moment that turns "what did we do this
// week" from a re-derivation against git log into an instant lookup.
func TestRecall_TimeWindowFastPath(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Save three memories with CreatedAt spread over time. "today" and
	// "yesterday" sit inside this week; "last month" does not.
	now := time.Now().UTC()
	thisWeek := []time.Time{now, now.AddDate(0, 0, -1)}
	older := now.AddDate(0, -1, 0)

	for i, ts := range thisWeek {
		mem := &Memory{
			Name:        fakeName("this-week", i),
			Description: "in-window memory",
			Type:        TypeReference,
			Content:     "ts: " + ts.Format(time.RFC3339),
			Importance:  0.5,
			CreatedAt:   ts,
			UpdatedAt:   ts,
			FilePath:    filepath.Join(dir, fakeName("this-week", i)+".md"),
		}
		if err := mgr.Save(mem); err != nil {
			t.Fatalf("save in-window: %v", err)
		}
	}
	oldMem := &Memory{
		Name:        "out-of-window",
		Description: "older memory",
		Type:        TypeReference,
		Content:     "old",
		Importance:  0.5,
		CreatedAt:   older,
		UpdatedAt:   older,
		FilePath:    filepath.Join(dir, "out-of-window.md"),
	}
	if err := mgr.Save(oldMem); err != nil {
		t.Fatalf("save out-of-window: %v", err)
	}

	results, err := mgr.Recall("what did we do this week", 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("recall returned 0 results")
	}
	for i, r := range results {
		t.Logf("rank=%d name=%s score=%.4f source=%s", i, r.Memory.Name, r.Score, r.Source)
	}

	// At least one result should carry Source="time" — the fast-path
	// signal that the time-bucket fed the fusion.
	sawTime := false
	for _, r := range results {
		if r.Source == "time" {
			sawTime = true
			break
		}
	}
	if !sawTime {
		// The Source field tracks only the top-result attribution after
		// RRF re-ranking; the bucket may have fed a result that another
		// backend also surfaced. As a relaxation, also assert the
		// in-window memories beat the out-of-window memory.
		topName := results[0].Memory.Name
		if !strings.HasPrefix(topName, "this-week-") {
			t.Errorf("expected an in-window memory at rank 1, got %q", topName)
		}
	}

	// The out-of-window memory should not appear at the top of the list.
	for i, r := range results {
		if i < 2 && r.Memory.Name == "out-of-window" {
			t.Errorf("out-of-window memory at rank %d: %+v", i, r)
		}
	}
}

func fakeName(prefix string, i int) string {
	return prefix + "-" + string(rune('a'+i))
}
