package agentmode

import (
	"strings"
	"testing"
)

// fixture is a multi-line JSONL string built by hand so we can pin the
// exact expected aggregates without depending on the on-disk encoder.
const fixture = `{"ts":"2026-05-09T10:00:00Z","phase":"pre","cmd":"git status","fired_ids":["git-log-status-diff-suggests-yc-git"]}
{"ts":"2026-05-09T10:00:01Z","phase":"pre","cmd":"git diff","fired_ids":["git-log-status-diff-suggests-yc-git"]}
{"ts":"2026-05-09T10:00:02Z","phase":"pre","cmd":"awk '/^func/' file.go"}
{"ts":"2026-05-09T10:00:03Z","phase":"pre","cmd":"awk -F: '{print $1}' /etc/passwd"}
{"ts":"2026-05-09T10:00:04Z","phase":"pre","cmd":"sed -n '1,10p' README.md"}
{"ts":"2026-05-09T10:00:05Z","phase":"pre","cmd":"grep -nE '^func' bar.go","fired_ids":["grep-source-file-suggests-symbols"]}
{"ts":"2026-05-09T10:00:06Z","phase":"post","exit_code":127,"fired_ids":["exit-127-suggests-yc-help"]}
malformed-line-should-be-skipped
{"ts":"2026-05-09T10:00:07Z","phase":"pre","cmd":"   ","fired_ids":[]}
`

func TestMissedGroupsByFirstToken(t *testing.T) {
	entries, err := Missed(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("Missed: %v", err)
	}
	// Expected: awk (2), sed (1). git/grep are hits; whitespace-only
	// command is dropped by normalize; post records ignored.
	want := map[string]int{"awk": 2, "sed": 1}
	got := map[string]int{}
	for _, e := range entries {
		got[e.Cmd] = e.Count
	}
	if len(got) != len(want) {
		t.Fatalf("entry count: want %d, got %d (%+v)", len(want), len(got), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("group %q: want %d, got %d", k, v, got[k])
		}
	}
	// awk should sort first (higher count).
	if entries[0].Cmd != "awk" {
		t.Errorf("expected awk first by frequency, got %+v", entries)
	}
}

func TestComputeStatsAggregates(t *testing.T) {
	s, err := ComputeStats(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if s.TotalRecords != 8 {
		t.Errorf("TotalRecords: want 8, got %d", s.TotalRecords)
	}
	if s.PreRecords != 7 {
		t.Errorf("PreRecords: want 7, got %d", s.PreRecords)
	}
	if s.PostRecords != 1 {
		t.Errorf("PostRecords: want 1, got %d", s.PostRecords)
	}
	if s.HitPre != 3 || s.MissPre != 4 {
		t.Errorf("pre hit/miss: want 3/4, got %d/%d", s.HitPre, s.MissPre)
	}
	wantRate := 3.0 / 7.0
	if abs(s.HitRatePre-wantRate) > 0.001 {
		t.Errorf("HitRatePre: want ~%.3f, got %.3f", wantRate, s.HitRatePre)
	}
	if s.ByID["git-log-status-diff-suggests-yc-git"] != 2 {
		t.Errorf("ByID[git rule] want 2, got %d", s.ByID["git-log-status-diff-suggests-yc-git"])
	}
	if s.ByID["grep-source-file-suggests-symbols"] != 1 {
		t.Errorf("ByID[grep rule] want 1, got %d", s.ByID["grep-source-file-suggests-symbols"])
	}
	if s.ByID["exit-127-suggests-yc-help"] != 1 {
		t.Errorf("ByID[post rule] want 1, got %d", s.ByID["exit-127-suggests-yc-help"])
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"   ":                "",
		"GIT status":         "git",
		"  grep foo bar.go ": "grep",
		"awk":                "awk",
		"awk\tBEGIN":         "awk",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q; want %q", in, got, want)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
