package memory

import (
	"path/filepath"
	"testing"
	"time"
)

// TestStore_OriginRoundtrip verifies that Origin, SourceQ, and
// LastVerifiedAt survive a save+load cycle through the frontmatter.
func TestStore_OriginRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	verified := time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)
	mem := &Memory{
		Name:        "roundtrip-origin",
		Description: "origin roundtrip",
		Type:        TypeReference,
		Content:     "body",
		FilePath:    filepath.Join(dir, "roundtrip-origin.md"),
		Origin: &Origin{
			PersonaID: "abc123",
			Host:      "macbook-pro",
			ProjectID: "github.com/qiangli/ycode",
			SessionID: "sess-42",
			AgentTool: "tui",
		},
		SourceQ:        "what did we do today",
		LastVerifiedAt: verified,
	}

	if err := store.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := store.Load(mem.FilePath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Origin == nil {
		t.Fatal("Origin lost in roundtrip")
	}
	if got := *loaded.Origin; got != *mem.Origin {
		t.Errorf("Origin mismatch: got %+v want %+v", got, *mem.Origin)
	}
	if loaded.SourceQ != mem.SourceQ {
		t.Errorf("SourceQ: got %q want %q", loaded.SourceQ, mem.SourceQ)
	}
	if !loaded.LastVerifiedAt.Equal(verified) {
		t.Errorf("LastVerifiedAt: got %v want %v", loaded.LastVerifiedAt, verified)
	}
}

// TestStore_OriginOmitEmpty verifies that memories without Origin produce
// frontmatter that does not contain origin_* keys (forward-compat for
// readers that do not know about Origin).
func TestStore_OriginOmitEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	mem := &Memory{
		Name:        "no-origin",
		Description: "plain",
		Type:        TypeUser,
		Content:     "body",
		FilePath:    filepath.Join(dir, "no-origin.md"),
	}
	if err := store.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := store.Load(mem.FilePath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Origin != nil {
		t.Errorf("expected nil Origin, got %+v", loaded.Origin)
	}
	if !loaded.LastVerifiedAt.IsZero() {
		t.Errorf("expected zero LastVerifiedAt, got %v", loaded.LastVerifiedAt)
	}
}

// fakeStamper sets Origin to a fixed value; used to assert that Save
// invokes the stamper before persistence.
type fakeStamper struct{ origin Origin }

func (f *fakeStamper) Stamp(mem *Memory) {
	if mem.Origin == nil {
		mem.Origin = &Origin{}
	}
	*mem.Origin = f.origin
}

func TestManager_SaveInvokesStamper(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	wantOrigin := Origin{PersonaID: "p1", Host: "h1", ProjectID: "proj1", SessionID: "s1", AgentTool: "tui"}
	mgr.SetStamper(&fakeStamper{origin: wantOrigin})

	mem := &Memory{
		Name:        "stamped",
		Description: "test",
		Type:        TypeReference,
		Content:     "x",
	}
	if err := mgr.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}
	if mem.Origin == nil || *mem.Origin != wantOrigin {
		t.Errorf("Origin not stamped: got %+v want %+v", mem.Origin, wantOrigin)
	}

	// Verify the stamp persisted to disk too.
	store := mgr.Store()
	loaded, err := store.Load(mem.FilePath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Origin == nil || *loaded.Origin != wantOrigin {
		t.Errorf("Origin not persisted: got %+v want %+v", loaded.Origin, wantOrigin)
	}
}

func TestFormatProvenance(t *testing.T) {
	mem := &Memory{
		Origin: &Origin{Host: "macbook", ProjectID: "github.com/qiangli/ycode"},
	}
	tests := []struct {
		name       string
		host, proj string
		mem        *Memory
		want       string
	}{
		{"different host", "ec2-dev", "github.com/qiangli/ycode", mem, "[macbook]"},
		{"different project", "macbook", "github.com/other/repo", mem, "[github.com/qiangli/ycode]"},
		{"both differ", "ec2-dev", "github.com/other/repo", mem,
			"[macbook · github.com/qiangli/ycode]"},
		{"same host and project", "macbook", "github.com/qiangli/ycode", mem, ""},
		{"nil origin", "macbook", "github.com/qiangli/ycode",
			&Memory{}, ""},
		{"nil memory", "macbook", "github.com/qiangli/ycode", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatProvenance(tt.mem, tt.host, tt.proj)
			if got != tt.want {
				t.Errorf("FormatProvenance = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestManager_StoreForScope_UserAndTeam(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	userStore, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new user store: %v", err)
	}
	teamStore, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new team store: %v", err)
	}
	mgr.SetUserStore(userStore)
	mgr.SetTeamStore(teamStore)

	if got := mgr.storeForScope(ScopeUser); got != userStore {
		t.Errorf("ScopeUser should route to user store, got %p (want %p)", got, userStore)
	}
	if got := mgr.storeForScope(ScopeTeam); got != teamStore {
		t.Errorf("ScopeTeam should route to team store, got %p (want %p)", got, teamStore)
	}
	if got := mgr.storeForScope(ScopeProject); got != mgr.projectStore {
		t.Errorf("ScopeProject should route to project store, got %p (want %p)", got, mgr.projectStore)
	}

	// Falls back to project when scope-store is nil.
	plainMgr, _ := NewManager(t.TempDir())
	if got := plainMgr.storeForScope(ScopeUser); got != plainMgr.projectStore {
		t.Errorf("unconfigured ScopeUser should fall back to project store")
	}
}

func TestDreamer_BackfillOrigin(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	// Save a pre-Stamper memory: no Origin.
	mem := &Memory{
		Name:        "pre-origin",
		Description: "saved before stamper existed",
		Type:        TypeReference,
		Content:     "body",
	}
	if err := mgr.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}
	if mem.Origin != nil {
		t.Fatal("memory unexpectedly has Origin before stamper attached")
	}

	// Now attach a stamper and run the Dreamer.
	mgr.SetStamper(&fakeStamper{origin: Origin{
		PersonaID: "p-x",
		Host:      "host-x",
		ProjectID: "proj-x",
		SessionID: "s-should-not-leak",
		AgentTool: "tui-should-not-leak",
	}})
	d := NewDreamer(mgr, true)
	if err := d.consolidate(); err != nil {
		t.Fatalf("consolidate: %v", err)
	}

	// Load fresh from disk to ensure persistence.
	loaded, err := mgr.Store().Load(mem.FilePath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Origin == nil {
		t.Fatal("back-fill did not populate Origin")
	}
	if loaded.Origin.PersonaID != "p-x" || loaded.Origin.Host != "host-x" || loaded.Origin.ProjectID != "proj-x" {
		t.Errorf("sticky fields mis-stamped: %+v", loaded.Origin)
	}
	if loaded.Origin.SessionID != "" || loaded.Origin.AgentTool != "" {
		t.Errorf("session-local fields should be empty after back-fill, got %+v", loaded.Origin)
	}

	// Second run should not re-write (idempotent).
	count := 0
	memories, _ := mgr.All()
	count = d.backfillOrigin(memories)
	if count != 0 {
		t.Errorf("expected 0 back-fills on second run, got %d", count)
	}
}
