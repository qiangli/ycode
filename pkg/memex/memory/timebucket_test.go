package memory

import (
	"reflect"
	"testing"
	"time"
)

func TestTimeBucket_RebuildAndRange(t *testing.T) {
	tb := NewTimeBucket()

	day := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
	}
	mems := []*Memory{
		{Name: "a", CreatedAt: day(2026, 5, 11)},
		{Name: "b", CreatedAt: day(2026, 5, 13)},
		{Name: "c", CreatedAt: day(2026, 5, 13), LastAccessedAt: day(2026, 5, 15)},
		{Name: "d", CreatedAt: day(2026, 4, 30)},
		{Name: "e", LastAccessedAt: day(2026, 5, 12)},
	}
	tb.Rebuild(mems)

	// "this week" 2026-05-11 .. 2026-05-18
	got := tb.Range(day(2026, 5, 11), day(2026, 5, 18))
	want := []string{"a", "b", "c", "e"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("this week: got %v, want %v", got, want)
	}

	// Single day.
	got = tb.Range(day(2026, 5, 13), day(2026, 5, 14))
	want = []string{"b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("single day: got %v, want %v", got, want)
	}

	// Empty range.
	if r := tb.Range(day(2026, 1, 1), day(2026, 1, 1)); r != nil {
		t.Errorf("zero range should return nil, got %v", r)
	}
}

func TestTimeBucket_AddRemove(t *testing.T) {
	tb := NewTimeBucket()
	day := func(d int) time.Time { return time.Date(2026, 5, d, 0, 0, 0, 0, time.UTC) }

	tb.Add(&Memory{Name: "x", CreatedAt: day(15)})
	if got := tb.Range(day(15), day(16)); len(got) != 1 || got[0] != "x" {
		t.Fatalf("after Add: got %v", got)
	}

	tb.Add(&Memory{Name: "y", LastAccessedAt: day(15)})
	if got := tb.Range(day(15), day(16)); len(got) != 2 {
		t.Fatalf("after second Add: got %v", got)
	}

	tb.Remove("x")
	if got := tb.Range(day(15), day(16)); len(got) != 1 || got[0] != "y" {
		t.Errorf("after Remove x: got %v", got)
	}

	tb.Remove("y")
	if got := tb.Range(day(15), day(16)); got != nil {
		t.Errorf("after Remove y: got %v", got)
	}
}

func TestTimeBucket_RangeAcrossDST(t *testing.T) {
	// DST jump in US/Eastern: 2026-03-08 02:00 → 03:00.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	tb := NewTimeBucket()
	tb.Add(&Memory{Name: "spring-forward", CreatedAt: time.Date(2026, 3, 8, 12, 0, 0, 0, loc)})
	start := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	end := time.Date(2026, 3, 9, 0, 0, 0, 0, loc)
	if got := tb.Range(start, end); len(got) != 1 {
		t.Errorf("DST-day bucket missed: got %v", got)
	}
}
