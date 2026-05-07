package netscan

import (
	"testing"
	"time"
)

func TestMerge_DedupesByIP(t *testing.T) {
	t1 := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC)

	existing := []Host{
		{IP: "192.168.1.10", Sources: []Source{SourceCache}, LastSeen: t1},
	}
	observed := []Host{
		{IP: "192.168.1.10", Name: "alpha.local", Port: 22, Service: "_ssh._tcp",
			Sources: []Source{SourceMDNS}, LastSeen: t2},
		{IP: "192.168.1.20", Name: "beta", Sources: []Source{SourceConnect}, LastSeen: t2},
	}
	got := Merge(existing, observed)

	if len(got) != 2 {
		t.Fatalf("got %d hosts, want 2", len(got))
	}
	// Most recent first, ties by IP.
	if got[0].IP != "192.168.1.10" && got[1].IP != "192.168.1.10" {
		t.Errorf("expected both hosts present; got %+v", got)
	}
	for _, h := range got {
		if h.IP == "192.168.1.10" {
			if h.Name != "alpha.local" {
				t.Errorf("name not filled in from observation: %+v", h)
			}
			if !h.HasSource(SourceCache) || !h.HasSource(SourceMDNS) {
				t.Errorf("expected merged sources, got %v", h.Sources)
			}
			if !h.LastSeen.Equal(t2) {
				t.Errorf("LastSeen should be advanced to t2, got %v", h.LastSeen)
			}
		}
	}
}

func TestMerge_SortsByRecency(t *testing.T) {
	tEarly := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tLate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	got := Merge(nil, []Host{
		{IP: "10.0.0.1", LastSeen: tEarly},
		{IP: "10.0.0.2", LastSeen: tLate},
		{IP: "10.0.0.3", LastSeen: tEarly}, // ties broken by IP ascending
	})
	if got[0].IP != "10.0.0.2" {
		t.Errorf("expected 10.0.0.2 first (most recent), got %s", got[0].IP)
	}
	if got[1].IP != "10.0.0.1" || got[2].IP != "10.0.0.3" {
		t.Errorf("tie ordering wrong: %v / %v", got[1].IP, got[2].IP)
	}
}

func TestMerge_IgnoresEmptyIP(t *testing.T) {
	got := Merge(nil, []Host{{IP: ""}, {IP: "10.0.0.1"}})
	if len(got) != 1 || got[0].IP != "10.0.0.1" {
		t.Errorf("empty IP should be dropped, got %+v", got)
	}
}
