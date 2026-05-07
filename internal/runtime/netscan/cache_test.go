package netscan

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(filepath.Join(dir, "cache.json"))

	now := time.Now().UTC().Truncate(time.Second)
	hosts := []Host{
		{IP: "10.0.0.1", Name: "alpha", Port: 22, Service: "_ssh._tcp",
			Sources: []Source{SourceMDNS}, SeenCount: 3, LastSeen: now},
		{IP: "10.0.0.2", Sources: []Source{SourceConnect}, LastSeen: now.Add(-1 * time.Hour)},
	}
	if err := c.Save(hosts, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := c.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d hosts, want 2", len(got))
	}
	for _, h := range got {
		if !h.HasSource(SourceCache) {
			t.Errorf("loaded host should be tagged SourceCache: %+v", h)
		}
	}
}

func TestCache_MaxAgeDrops(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(filepath.Join(dir, "cache.json"))
	now := time.Now().UTC()
	hosts := []Host{
		{IP: "10.0.0.1", LastSeen: now},
		{IP: "10.0.0.2", LastSeen: now.Add(-48 * time.Hour)},
	}
	if err := c.Save(hosts, 24*time.Hour); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ := c.Load()
	if len(got) != 1 || got[0].IP != "10.0.0.1" {
		t.Errorf("expected only the recent host, got %+v", got)
	}
}

func TestMergeAndStamp_BumpsSeenCount(t *testing.T) {
	now := time.Now().UTC()
	cached := []Host{
		{IP: "10.0.0.1", SeenCount: 5, Sources: []Source{SourceCache},
			LastSeen: now.Add(-time.Hour)},
	}
	fresh := []Host{
		{IP: "10.0.0.1", Sources: []Source{SourceMDNS}, LastSeen: now},
		{IP: "10.0.0.2", Sources: []Source{SourceMDNS}, LastSeen: now},
	}
	got := MergeAndStamp(cached, fresh)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	for _, h := range got {
		switch h.IP {
		case "10.0.0.1":
			if h.SeenCount != 6 {
				t.Errorf("re-observed host SeenCount = %d, want 6", h.SeenCount)
			}
		case "10.0.0.2":
			if h.SeenCount != 1 {
				t.Errorf("new host SeenCount = %d, want 1", h.SeenCount)
			}
		}
	}
}
