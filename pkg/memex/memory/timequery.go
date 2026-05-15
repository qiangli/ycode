package memory

import (
	"regexp"
	"strings"
	"time"
)

// TimeWindow is a half-open interval [Start, End) for time-shaped queries.
type TimeWindow struct {
	Start time.Time
	End   time.Time
	// Label is a short human-readable description of the window
	// (e.g., "today", "this week", "2026-05-15"). Useful for the
	// recall header that tells the agent which window matched.
	Label string
}

// isoDate matches YYYY-MM-DD; the regex is intentionally strict so we
// don't false-fire on version numbers like "2.0.1".
var isoDate = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)

// DetectTimeWindow inspects a free-text query for time-shaped intent and
// returns the corresponding window or nil when no time signal is present.
// "now" is captured via the closure so tests can pin the clock.
func DetectTimeWindow(query string) *TimeWindow {
	return DetectTimeWindowAt(query, time.Now())
}

// DetectTimeWindowAt is the deterministic variant for testing; callers pass
// the reference "now".
func DetectTimeWindowAt(query string, now time.Time) *TimeWindow {
	q := strings.ToLower(query)
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	switch {
	case containsPhrase(q, "yesterday"):
		start := today.AddDate(0, 0, -1)
		return &TimeWindow{Start: start, End: today, Label: "yesterday"}
	case containsPhrase(q, "today"):
		return &TimeWindow{Start: today, End: today.AddDate(0, 0, 1), Label: "today"}
	case containsPhrase(q, "tomorrow"):
		start := today.AddDate(0, 0, 1)
		return &TimeWindow{Start: start, End: start.AddDate(0, 0, 1), Label: "tomorrow"}
	case containsPhrase(q, "last week"):
		start := startOfWeek(today, loc).AddDate(0, 0, -7)
		return &TimeWindow{Start: start, End: start.AddDate(0, 0, 7), Label: "last week"}
	case containsPhrase(q, "this week"):
		start := startOfWeek(today, loc)
		return &TimeWindow{Start: start, End: start.AddDate(0, 0, 7), Label: "this week"}
	case containsPhrase(q, "next week"):
		start := startOfWeek(today, loc).AddDate(0, 0, 7)
		return &TimeWindow{Start: start, End: start.AddDate(0, 0, 7), Label: "next week"}
	case containsPhrase(q, "last month"):
		start := startOfMonth(today.AddDate(0, -1, 0))
		return &TimeWindow{Start: start, End: startOfMonth(today), Label: "last month"}
	case containsPhrase(q, "this month"):
		start := startOfMonth(today)
		return &TimeWindow{Start: start, End: start.AddDate(0, 1, 0), Label: "this month"}
	case containsPhrase(q, "next month"):
		start := startOfMonth(today.AddDate(0, 1, 0))
		return &TimeWindow{Start: start, End: start.AddDate(0, 1, 0), Label: "next month"}
	case containsPhrase(q, "last year"):
		start := time.Date(now.Year()-1, 1, 1, 0, 0, 0, 0, loc)
		return &TimeWindow{Start: start, End: start.AddDate(1, 0, 0), Label: "last year"}
	case containsPhrase(q, "this year"):
		start := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		return &TimeWindow{Start: start, End: start.AddDate(1, 0, 0), Label: "this year"}
	}

	if m := isoDate.FindStringSubmatch(query); m != nil {
		if t, err := time.ParseInLocation("2006-01-02", m[0], loc); err == nil {
			return &TimeWindow{Start: t, End: t.AddDate(0, 0, 1), Label: m[0]}
		}
	}
	return nil
}

// containsPhrase looks for a phrase as a token boundary so "this week"
// does not falsely match "thiswkek" or "this weekend" (the latter is a
// separate concept — we don't want a week-window for "weekend").
func containsPhrase(haystack, needle string) bool {
	idx := strings.Index(haystack, needle)
	if idx < 0 {
		return false
	}
	// Boundary check on both sides.
	if idx > 0 && isWordByte(haystack[idx-1]) {
		return false
	}
	end := idx + len(needle)
	if end < len(haystack) && isWordByte(haystack[end]) {
		return false
	}
	return true
}

func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_':
		return true
	}
	return false
}

// startOfWeek returns Monday 00:00 of the week containing day.
func startOfWeek(day time.Time, loc *time.Location) time.Time {
	wd := int(day.Weekday())
	if wd == 0 {
		wd = 7 // treat Sunday as end-of-week
	}
	start := day.AddDate(0, 0, -(wd - 1))
	return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
}

func startOfMonth(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, day.Location())
}
