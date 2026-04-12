package cluster

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	defaultHeartbeatInterval = 5 * time.Second
	defaultStaleThreshold    = 30 * time.Second
)

// MemberInfo is the JSON written to each member's file.
type MemberInfo struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
	Heartbeat time.Time `json:"heartbeat"`
	Role      Role      `json:"role"`
}

// memberManager handles filesystem-based member registration and heartbeat.
type memberManager struct {
	membersDir string
	instanceID string
	filePath   string
	startedAt  time.Time
}

func newMemberManager(baseDir, instanceID string) *memberManager {
	dir := filepath.Join(baseDir, "members")
	return &memberManager{
		membersDir: dir,
		instanceID: instanceID,
		filePath:   filepath.Join(dir, instanceID+".json"),
		startedAt:  time.Now(),
	}
}

// register writes the member file.
func (m *memberManager) register(role Role) error {
	if err := os.MkdirAll(m.membersDir, 0o755); err != nil {
		return err
	}
	return m.writeInfo(role)
}

// deregister removes the member file.
func (m *memberManager) deregister() {
	os.Remove(m.filePath)
}

// writeInfo writes current member info to the file.
func (m *memberManager) writeInfo(role Role) error {
	info := MemberInfo{
		ID:        m.instanceID,
		PID:       os.Getpid(),
		StartedAt: m.startedAt,
		Heartbeat: time.Now(),
		Role:      role,
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(m.filePath, data, 0o644)
}

// listMembers reads all member files from the members directory.
func (m *memberManager) listMembers() ([]MemberInfo, error) {
	entries, err := os.ReadDir(m.membersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var members []MemberInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.membersDir, e.Name()))
		if err != nil {
			continue
		}
		var info MemberInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}
		members = append(members, info)
	}
	return members, nil
}

// cleanStale removes member files with heartbeats older than threshold.
func (m *memberManager) cleanStale(threshold time.Duration) {
	entries, err := os.ReadDir(m.membersDir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(m.membersDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var info MemberInfo
		if err := json.Unmarshal(data, &info); err != nil {
			os.Remove(path)
			continue
		}
		if info.ID != m.instanceID && now.Sub(info.Heartbeat) > threshold {
			slog.Info("cluster: removing stale member", "id", info.ID, "age", now.Sub(info.Heartbeat))
			os.Remove(path)
		}
	}
}
