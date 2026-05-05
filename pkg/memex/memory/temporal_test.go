package memory

import (
	"testing"
	"time"
)

func TestSupersedeMemory(t *testing.T) {
	old := &Memory{
		Name:        "old-fact",
		Description: "deploy target is staging-2",
		Type:        TypeProject,
		Content:     "Deploy to staging-2",
		UpdatedAt:   time.Now(),
	}

	SupersedeMemory(old, "new-fact")

	if old.ValidUntil == nil {
		t.Fatal("ValidUntil should be set")
	}
	if old.SupersededBy != "new-fact" {
		t.Errorf("SupersededBy = %q, want new-fact", old.SupersededBy)
	}
}

func TestIsValid(t *testing.T) {
	now := time.Now()
	pastHour := now.Add(-time.Hour)
	futureHour := now.Add(time.Hour)

	tests := []struct {
		name  string
		mem   *Memory
		valid bool
	}{
		{"no constraints", &Memory{}, true},
		{"valid_from in past", &Memory{ValidFrom: &pastHour}, true},
		{"valid_from in future", &Memory{ValidFrom: &futureHour}, false},
		{"valid_until in future", &Memory{ValidUntil: &futureHour}, true},
		{"valid_until in past", &Memory{ValidUntil: &pastHour}, false},
		{"valid window current", &Memory{ValidFrom: &pastHour, ValidUntil: &futureHour}, true},
		{"valid window expired", &Memory{ValidFrom: &pastHour, ValidUntil: &pastHour}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsValid(tc.mem); got != tc.valid {
				t.Errorf("IsValid = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestIsSuperseded(t *testing.T) {
	normal := &Memory{Name: "active"}
	if IsSuperseded(normal) {
		t.Error("normal memory should not be superseded")
	}

	replaced := &Memory{Name: "old", SupersededBy: "new"}
	if !IsSuperseded(replaced) {
		t.Error("memory with SupersededBy should be superseded")
	}
}

func TestIsStale_RespectsValidUntil(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	mem := &Memory{
		Name:       "expired-fact",
		Type:       TypeUser,
		UpdatedAt:  time.Now(), // fresh by age
		ValidUntil: &past,      // but expired by validity
	}

	if !IsStale(mem) {
		t.Error("memory past ValidUntil should be stale regardless of age")
	}
}

func TestTemporalFieldsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	future := now.Add(24 * time.Hour)

	mem := &Memory{
		Name:         "temporal-test",
		Description:  "test temporal fields",
		Type:         TypeProject,
		Content:      "content",
		ValidFrom:    &now,
		ValidUntil:   &future,
		SupersededBy: "newer-mem",
	}

	if err := store.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load(mem.FilePath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.ValidFrom == nil {
		t.Fatal("ValidFrom should be preserved")
	}
	if loaded.ValidFrom.Unix() != now.Unix() {
		t.Errorf("ValidFrom = %v, want %v", loaded.ValidFrom, now)
	}
	if loaded.ValidUntil == nil {
		t.Fatal("ValidUntil should be preserved")
	}
	if loaded.ValidUntil.Unix() != future.Unix() {
		t.Errorf("ValidUntil = %v, want %v", loaded.ValidUntil, future)
	}
	if loaded.SupersededBy != "newer-mem" {
		t.Errorf("SupersededBy = %q, want newer-mem", loaded.SupersededBy)
	}
}

func TestValueAndTagsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	mem := &Memory{
		Name:        "value-test",
		Description: "test value and tags",
		Type:        TypeUser,
		Content:     "content",
		ValueScore:  0.75,
		AccessCount: 3,
		Tags:        []string{"go", "memory"},
		Entities:    []string{"ycode", "memory-system"},
	}

	if err := store.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load(mem.FilePath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.ValueScore != 0.75 {
		t.Errorf("ValueScore = %f, want 0.75", loaded.ValueScore)
	}
	if loaded.AccessCount != 3 {
		t.Errorf("AccessCount = %d, want 3", loaded.AccessCount)
	}
	if len(loaded.Tags) != 2 || loaded.Tags[0] != "go" {
		t.Errorf("Tags = %v, want [go memory]", loaded.Tags)
	}
	if len(loaded.Entities) != 2 || loaded.Entities[0] != "ycode" {
		t.Errorf("Entities = %v, want [ycode memory-system]", loaded.Entities)
	}
}
