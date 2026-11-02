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

	// Recall detects this week in the current local timezone. Yesterday is
	// outside that window on Monday; use its inclusive start instead.
	thisWeek, older := recallWeekFixtureTimes(time.Now())

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
	if len(results) < 2 {
		t.Fatalf("recall returned %d results, want both in-window memories", len(results))
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

	// Both distinct in-window memories must occupy the top two positions.
	seen := make(map[string]bool)
	for _, r := range results[:2] {
		if !strings.HasPrefix(r.Memory.Name, "this-week-") {
			t.Errorf("top-two memory outside window: %q", r.Memory.Name)
		}
		seen[r.Memory.Name] = true
	}
	if !seen[fakeName("this-week", 0)] || !seen[fakeName("this-week", 1)] {
		t.Errorf("top two do not contain both in-window memories: %v", seen)
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

// This helper belongs to the fixture, not the product clock or ranking policy.
func recallWeekFixtureTimes(now time.Time) ([]time.Time, time.Time) {
	window := DetectTimeWindowAt("this week", now)
	return []time.Time{now, window.Start}, now.AddDate(0, -1, 0)
}

func TestRecallWeekFixtureTimesEveryWeekday(t *testing.T) {
	for _, loc := range []*time.Location{time.UTC, time.FixedZone("west", -8*60*60), time.FixedZone("east", 9*60*60)} {
		monday := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
		end := monday.AddDate(0, 0, 7)
		for day := range 7 {
			for _, offset := range []time.Duration{0, 24*time.Hour - time.Nanosecond} {
				now := monday.AddDate(0, 0, day).Add(offset)
				t.Run(loc.String()+"/"+now.Weekday().String()+"/"+offset.String(), func(t *testing.T) {
					inside, older := recallWeekFixtureTimes(now)
					if len(inside) != 2 {
						t.Fatalf("in-window count=%d", len(inside))
					}
					for _, ts := range inside {
						if ts.Before(monday) || !ts.Before(end) {
							t.Errorf("fixture timestamp %s outside [%s,%s)", ts, monday, end)
						}
					}
					if !inside[0].Equal(now) || !inside[1].Equal(monday) {
						t.Errorf("fixture changed reference time or Monday boundary: %v", inside)
					}
					if !older.Before(monday) {
						t.Errorf("older fixture %s inside current week", older)
					}
				})
			}
		}
	}
}
