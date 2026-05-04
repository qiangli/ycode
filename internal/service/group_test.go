package service

import (
	"testing"
)

func TestGroupManager_CreateAndGet(t *testing.T) {
	gm := NewGroupManager()

	g, err := gm.Create("team-1", "Team Alpha")
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != "team-1" || g.Name != "Team Alpha" {
		t.Errorf("unexpected group: %+v", g)
	}

	got := gm.Get("team-1")
	if got == nil {
		t.Fatal("expected group")
	}
	if got.Name != "Team Alpha" {
		t.Errorf("expected Team Alpha, got %s", got.Name)
	}

	// Duplicate create fails.
	_, err = gm.Create("team-1", "Dup")
	if err == nil {
		t.Error("expected error on duplicate create")
	}
}

func TestGroupManager_AddRemoveSession(t *testing.T) {
	gm := NewGroupManager()

	gm.AddSession("team-1", "s1")
	gm.AddSession("team-1", "s2")
	gm.AddSession("team-1", "s1") // duplicate — should be ignored

	g := gm.Get("team-1")
	if g == nil {
		t.Fatal("expected group")
	}
	if len(g.Sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(g.Sessions))
	}

	gm.RemoveSession("team-1", "s1")
	g = gm.Get("team-1")
	if len(g.Sessions) != 1 || g.Sessions[0] != "s2" {
		t.Errorf("expected [s2], got %v", g.Sessions)
	}

	// Removing last session deletes the group.
	gm.RemoveSession("team-1", "s2")
	if gm.Get("team-1") != nil {
		t.Error("expected group to be deleted after last session removed")
	}
}

func TestGroupManager_SessionGroups(t *testing.T) {
	gm := NewGroupManager()

	gm.AddSession("team-1", "s1")
	gm.AddSession("team-2", "s1")
	gm.AddSession("team-2", "s2")

	groups := gm.SessionGroups("s1")
	if len(groups) != 2 {
		t.Errorf("expected s1 in 2 groups, got %d", len(groups))
	}

	groups = gm.SessionGroups("s2")
	if len(groups) != 1 {
		t.Errorf("expected s2 in 1 group, got %d", len(groups))
	}

	groups = gm.SessionGroups("nonexistent")
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestGroupManager_Delete(t *testing.T) {
	gm := NewGroupManager()
	gm.AddSession("team-1", "s1")
	gm.Delete("team-1")

	if gm.Get("team-1") != nil {
		t.Error("expected group deleted")
	}
}

func TestGroupManager_List(t *testing.T) {
	gm := NewGroupManager()
	gm.AddSession("team-1", "s1")
	gm.AddSession("team-2", "s2")

	groups := gm.List()
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}
}
