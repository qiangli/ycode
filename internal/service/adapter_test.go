package service

import (
	"testing"
)

func TestExternalSessionMap_SetGet(t *testing.T) {
	m := NewExternalSessionMap()

	m.Set("slack:W1:C1", "session-1")
	m.Set("discord:G1:C1", "session-2")

	if got := m.Get("slack:W1:C1"); got != "session-1" {
		t.Errorf("expected session-1, got %q", got)
	}
	if got := m.Get("discord:G1:C1"); got != "session-2" {
		t.Errorf("expected session-2, got %q", got)
	}
	if got := m.Get("nonexistent"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExternalSessionMap_Reverse(t *testing.T) {
	m := NewExternalSessionMap()
	m.Set("slack:W1:C1", "session-1")

	if got := m.GetExternal("session-1"); got != "slack:W1:C1" {
		t.Errorf("expected slack:W1:C1, got %q", got)
	}
	if got := m.GetExternal("nonexistent"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExternalSessionMap_Remove(t *testing.T) {
	m := NewExternalSessionMap()
	m.Set("slack:W1:C1", "session-1")
	m.Remove("slack:W1:C1")

	if got := m.Get("slack:W1:C1"); got != "" {
		t.Errorf("expected empty after remove, got %q", got)
	}
	if got := m.GetExternal("session-1"); got != "" {
		t.Errorf("expected empty reverse after remove, got %q", got)
	}
}
