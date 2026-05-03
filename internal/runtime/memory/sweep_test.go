package memory

import (
	"testing"
	"time"
)

func TestSweeper_RemovesExpiredMemories(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)

	// Save an expired memory.
	err = mgr.Save(&Memory{
		Name:       "expired",
		Type:       TypeProject,
		Content:    "old data",
		ValidUntil: &past,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Save a non-expired memory.
	err = mgr.Save(&Memory{
		Name:       "active",
		Type:       TypeProject,
		Content:    "fresh data",
		ValidUntil: &future,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Save a memory with no expiration.
	err = mgr.Save(&Memory{
		Name:    "permanent",
		Type:    TypeUser,
		Content: "always here",
	})
	if err != nil {
		t.Fatal(err)
	}

	sweeper := NewSweeper(mgr, DefaultSweepInterval)
	sweeper.SweepOnce()

	all, err := mgr.All()
	if err != nil {
		t.Fatal(err)
	}

	names := make(map[string]bool)
	for _, m := range all {
		names[m.Name] = true
	}

	if names["expired"] {
		t.Error("expired memory should have been removed")
	}
	if !names["active"] {
		t.Error("active memory should still exist")
	}
	if !names["permanent"] {
		t.Error("permanent memory should still exist")
	}
}

func TestSweeper_StartStop(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	sweeper := NewSweeper(mgr, 1*time.Hour)
	sweeper.Start()
	sweeper.Start() // second start is no-op
	sweeper.Stop()
	sweeper.Stop() // second stop is no-op
}

func TestTTLMinutes_AutoSetsValidUntil(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	mem := &Memory{
		Name:       "ttl-test",
		Type:       TypeProject,
		Content:    "expires soon",
		TTLMinutes: 30,
	}

	before := time.Now()
	err = store.Save(mem)
	if err != nil {
		t.Fatal(err)
	}

	if mem.ValidUntil == nil {
		t.Fatal("ValidUntil should be set from TTLMinutes")
	}

	expectedMin := before.Add(30 * time.Minute)
	expectedMax := time.Now().Add(30 * time.Minute)

	if mem.ValidUntil.Before(expectedMin) || mem.ValidUntil.After(expectedMax) {
		t.Errorf("ValidUntil %v not in expected range [%v, %v]",
			mem.ValidUntil, expectedMin, expectedMax)
	}
}

func TestTTLMinutes_DoesNotOverrideExplicitValidUntil(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	explicit := time.Now().Add(24 * time.Hour)
	mem := &Memory{
		Name:       "explicit-ttl",
		Type:       TypeProject,
		Content:    "has explicit expiry",
		TTLMinutes: 30,
		ValidUntil: &explicit,
	}

	err = store.Save(mem)
	if err != nil {
		t.Fatal(err)
	}

	// ValidUntil should remain as explicitly set, not overridden by TTL.
	if !mem.ValidUntil.Equal(explicit) {
		t.Errorf("ValidUntil should be %v (explicit), got %v", explicit, mem.ValidUntil)
	}
}

func TestTTLMinutes_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	mem := &Memory{
		Name:       "ttl-roundtrip",
		Type:       TypeProject,
		Content:    "test roundtrip",
		TTLMinutes: 60,
	}
	if err := store.Save(mem); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load(mem.FilePath)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.TTLMinutes != 60 {
		t.Errorf("TTLMinutes roundtrip: got %d, want 60", loaded.TTLMinutes)
	}
	if loaded.ValidUntil == nil {
		t.Error("ValidUntil should be set after roundtrip")
	}
}
