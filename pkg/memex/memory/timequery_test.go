package memory

import (
	"testing"
	"time"
)

func TestDetectTimeWindow(t *testing.T) {
	// Fix "now" at Friday 2026-05-15 14:30 UTC.
	now := time.Date(2026, 5, 15, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name      string
		query     string
		wantNil   bool
		wantLabel string
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "today",
			query:     "what did we do today",
			wantLabel: "today",
			wantStart: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "yesterday",
			query:     "yesterday's commits",
			wantLabel: "yesterday",
			wantStart: time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "this week",
			query:     "what did we ship this week",
			wantLabel: "this week",
			// Week starts Monday 2026-05-11.
			wantStart: time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "last week",
			query:     "Last Week wins",
			wantLabel: "last week",
			wantStart: time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "this month",
			query:     "this month so far",
			wantLabel: "this month",
			wantStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "last month",
			query:     "what shipped last month",
			wantLabel: "last month",
			wantStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "iso date",
			query:     "what happened on 2026-05-14?",
			wantLabel: "2026-05-14",
			wantStart: time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "version number is not a date",
			query:   "release v1.2.3",
			wantNil: true,
		},
		{
			name:    "no time signal",
			query:   "how does the parser work",
			wantNil: true,
		},
		{
			name:    "weekend should not match this week",
			query:   "weekend plans",
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectTimeWindowAt(tt.query, now)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil window, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil window")
			}
			if got.Label != tt.wantLabel {
				t.Errorf("Label = %q, want %q", got.Label, tt.wantLabel)
			}
			if !got.Start.Equal(tt.wantStart) {
				t.Errorf("Start = %v, want %v", got.Start, tt.wantStart)
			}
			if !got.End.Equal(tt.wantEnd) {
				t.Errorf("End = %v, want %v", got.End, tt.wantEnd)
			}
		})
	}
}

// Sunday-edge: confirm Sunday is treated as the *end* of "this week"
// rather than the start. ycode's audience tends to think Mon-start.
func TestDetectTimeWindow_SundayEdge(t *testing.T) {
	sunday := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC) // Sun
	got := DetectTimeWindowAt("this week", sunday)
	if got == nil {
		t.Fatal("expected window")
	}
	wantStart := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC) // Mon
	wantEnd := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	if !got.Start.Equal(wantStart) || !got.End.Equal(wantEnd) {
		t.Errorf("Sunday-of-this-week mis-bucketed: got [%v, %v) want [%v, %v)",
			got.Start, got.End, wantStart, wantEnd)
	}
}
