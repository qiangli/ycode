package service

import (
	"fmt"
	"log/slog"
	"sync"
)

// GroupManager manages groups of sessions for team agent coordination.
// Groups enable cross-session event delivery — when an event is published
// with a GroupID, it reaches all clients in all sessions belonging to that group.
type GroupManager struct {
	mu     sync.RWMutex
	groups map[string]*Group // groupID → Group
}

// Group is a named collection of session IDs.
type Group struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Sessions []string `json:"sessions"`
}

// NewGroupManager creates a new group manager.
func NewGroupManager() *GroupManager {
	return &GroupManager{
		groups: make(map[string]*Group),
	}
}

// Create creates a new group with the given ID and optional name.
func (gm *GroupManager) Create(id, name string) (*Group, error) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	if _, ok := gm.groups[id]; ok {
		return nil, fmt.Errorf("group %q already exists", id)
	}
	g := &Group{ID: id, Name: name}
	gm.groups[id] = g
	slog.Info("group: created", "id", id, "name", name)
	return g, nil
}

// Get returns a group by ID, or nil if not found.
func (gm *GroupManager) Get(id string) *Group {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	g, ok := gm.groups[id]
	if !ok {
		return nil
	}
	// Return a copy so callers can't mutate internal state.
	cp := *g
	cp.Sessions = append([]string{}, g.Sessions...)
	return &cp
}

// List returns all groups.
func (gm *GroupManager) List() []*Group {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	result := make([]*Group, 0, len(gm.groups))
	for _, g := range gm.groups {
		cp := *g
		cp.Sessions = append([]string{}, g.Sessions...)
		result = append(result, &cp)
	}
	return result
}

// AddSession adds a session to a group. Creates the group if it doesn't exist.
func (gm *GroupManager) AddSession(groupID, sessionID string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	g, ok := gm.groups[groupID]
	if !ok {
		g = &Group{ID: groupID}
		gm.groups[groupID] = g
	}
	// Avoid duplicates.
	for _, s := range g.Sessions {
		if s == sessionID {
			return
		}
	}
	g.Sessions = append(g.Sessions, sessionID)
	slog.Debug("group: added session", "group", groupID, "session", sessionID)
}

// RemoveSession removes a session from a group.
func (gm *GroupManager) RemoveSession(groupID, sessionID string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	g, ok := gm.groups[groupID]
	if !ok {
		return
	}
	for i, s := range g.Sessions {
		if s == sessionID {
			g.Sessions = append(g.Sessions[:i], g.Sessions[i+1:]...)
			break
		}
	}
	// Remove empty groups.
	if len(g.Sessions) == 0 {
		delete(gm.groups, groupID)
	}
}

// Delete removes a group entirely.
func (gm *GroupManager) Delete(groupID string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	delete(gm.groups, groupID)
}

// SessionGroups returns all group IDs that a session belongs to.
func (gm *GroupManager) SessionGroups(sessionID string) []string {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	var groups []string
	for _, g := range gm.groups {
		for _, s := range g.Sessions {
			if s == sessionID {
				groups = append(groups, g.ID)
				break
			}
		}
	}
	return groups
}
