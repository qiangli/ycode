package detector

import (
	"testing"
	"time"
)

func TestDedupe_FirstSeenAndCount(t *testing.T) {
	d := NewDedupe(time.Hour, nil)
	first, count := d.See("sig-a")
	if !first || count != 1 {
		t.Fatalf("first sighting = (first=%v, count=%d); want (true, 1)", first, count)
	}
	first, count = d.See("sig-a")
	if first || count != 2 {
		t.Fatalf("second sighting = (first=%v, count=%d); want (false, 2)", first, count)
	}
	first, count = d.See("sig-b")
	if !first || count != 1 {
		t.Fatalf("new signature should be firstSeen=true count=1; got (%v, %d)", first, count)
	}
}

func TestDedupe_EvictionByTTL(t *testing.T) {
	clock := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	d := NewDedupe(time.Minute, func() time.Time { return clock })

	first, _ := d.See("sig-x")
	if !first {
		t.Fatal("expected first sighting")
	}
	// Advance past TTL: the entry should be evicted on the next See.
	clock = clock.Add(2 * time.Minute)
	first, count := d.See("sig-x")
	if !first {
		t.Fatalf("after TTL expiry, signature should re-fire as first; got first=%v count=%d", first, count)
	}
	if d.Size() != 1 {
		t.Fatalf("dedupe size = %d; want 1 (old entry evicted, new one added)", d.Size())
	}
}
