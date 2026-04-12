//go:build unix

package cluster

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemberRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := t.TempDir()
	mm := newMemberManager(dir, "test-instance-1")

	if err := mm.register(RoleStandby); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Verify file exists.
	data, err := os.ReadFile(mm.filePath)
	if err != nil {
		t.Fatalf("read member file: %v", err)
	}
	var info MemberInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if info.ID != "test-instance-1" {
		t.Errorf("id = %q, want %q", info.ID, "test-instance-1")
	}
	if info.Role != RoleStandby {
		t.Errorf("role = %d, want %d", info.Role, RoleStandby)
	}

	// List members.
	members, err := mm.listMembers()
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("members count = %d, want 1", len(members))
	}

	// Deregister.
	mm.deregister()
	if _, err := os.Stat(mm.filePath); !os.IsNotExist(err) {
		t.Error("member file still exists after deregister")
	}
}

func TestStaleCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := t.TempDir()
	mm := newMemberManager(dir, "current")
	mm.register(RoleStandby)

	// Write a fake stale member file.
	staleInfo := MemberInfo{
		ID:        "stale-member",
		PID:       99999,
		StartedAt: time.Now().Add(-1 * time.Hour),
		Heartbeat: time.Now().Add(-1 * time.Minute),
		Role:      RoleStandby,
	}
	data, _ := json.Marshal(staleInfo)
	stalePath := filepath.Join(dir, "members", "stale-member.json")
	os.WriteFile(stalePath, data, 0o644)

	mm.cleanStale(30 * time.Second)

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Error("stale member file not cleaned up")
	}
	// Current member should still exist.
	if _, err := os.Stat(mm.filePath); err != nil {
		t.Error("current member file was removed")
	}
}

func TestElectionAcquireRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := t.TempDir()
	e := newElection(dir)

	acquired, err := e.tryAcquire()
	if err != nil {
		t.Fatalf("tryAcquire: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}
	if !e.isLeader {
		t.Error("expected isLeader=true")
	}

	// Second acquire on same election should return true (already holding).
	acquired2, err := e.tryAcquire()
	if err != nil {
		t.Fatalf("second tryAcquire: %v", err)
	}
	if !acquired2 {
		t.Error("expected already-holding to return true")
	}

	// Another election on same dir should fail.
	e2 := newElection(dir)
	acquired3, err := e2.tryAcquire()
	if err != nil {
		t.Fatalf("e2 tryAcquire: %v", err)
	}
	if acquired3 {
		t.Error("expected second election to fail")
	}

	// Release first, then second should succeed.
	e.release()
	acquired4, err := e2.tryAcquire()
	if err != nil {
		t.Fatalf("e2 retry: %v", err)
	}
	if !acquired4 {
		t.Error("expected second election to succeed after release")
	}
	e2.release()
}

func TestClusterSingleInstance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := t.TempDir()

	var promoted atomic.Bool
	var demoted atomic.Bool

	cl := New(dir, "instance-1", Options{
		NATSPort:          0, // will use default 4222; test uses unique dirs for lock isolation
		RetryInterval:     500 * time.Millisecond,
		HeartbeatInterval: 500 * time.Millisecond,
		StaleThreshold:    5 * time.Second,
		OnPromoted: func(ctx context.Context) error {
			promoted.Store(true)
			return nil
		},
		OnDemoted: func(ctx context.Context) error {
			demoted.Store(true)
			return nil
		},
	})

	ctx := context.Background()
	if err := cl.Join(ctx); err != nil {
		t.Fatalf("join: %v", err)
	}

	// Wait for promotion.
	deadline := time.After(10 * time.Second)
	for !promoted.Load() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for promotion")
		case <-time.After(100 * time.Millisecond):
		}
	}

	if cl.Role() != RoleMaster {
		t.Errorf("role = %v, want master", cl.Role())
	}

	// Leave should trigger demotion.
	leaveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := cl.Leave(leaveCtx); err != nil {
		t.Fatalf("leave: %v", err)
	}

	if !demoted.Load() {
		t.Error("expected demotion callback")
	}
}

func TestClusterFailover(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := t.TempDir()

	// Use a non-default port to avoid conflicts.
	port := 14222

	var promoted1 atomic.Bool
	var promoted2 atomic.Bool

	cl1 := New(dir, "instance-1", Options{
		NATSPort:          port,
		RetryInterval:     500 * time.Millisecond,
		HeartbeatInterval: 500 * time.Millisecond,
		OnPromoted: func(ctx context.Context) error {
			promoted1.Store(true)
			return nil
		},
		OnDemoted: func(ctx context.Context) error { return nil },
	})

	cl2 := New(dir, "instance-2", Options{
		NATSPort:          port,
		RetryInterval:     500 * time.Millisecond,
		HeartbeatInterval: 500 * time.Millisecond,
		OnPromoted: func(ctx context.Context) error {
			promoted2.Store(true)
			return nil
		},
		OnDemoted: func(ctx context.Context) error { return nil },
	})

	ctx := context.Background()

	// Start instance 1 — should become master.
	if err := cl1.Join(ctx); err != nil {
		t.Fatalf("cl1 join: %v", err)
	}
	waitFor(t, &promoted1, 10*time.Second, "cl1 promotion")

	if cl1.Role() != RoleMaster {
		t.Fatalf("cl1 role = %v, want master", cl1.Role())
	}

	// Start instance 2 — should be standby.
	if err := cl2.Join(ctx); err != nil {
		t.Fatalf("cl2 join: %v", err)
	}
	time.Sleep(time.Second) // give cl2 time to settle
	if cl2.Role() != RoleStandby {
		t.Fatalf("cl2 role = %v, want standby", cl2.Role())
	}

	// Leave instance 1 — instance 2 should take over.
	leaveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := cl1.Leave(leaveCtx); err != nil {
		t.Fatalf("cl1 leave: %v", err)
	}

	waitFor(t, &promoted2, 10*time.Second, "cl2 promotion after failover")

	if cl2.Role() != RoleMaster {
		t.Errorf("cl2 role after failover = %v, want master", cl2.Role())
	}

	// Cleanup.
	leaveCtx2, cancel2 := context.WithTimeout(ctx, 10*time.Second)
	defer cancel2()
	cl2.Leave(leaveCtx2)
}

func waitFor(t *testing.T, flag *atomic.Bool, timeout time.Duration, desc string) {
	t.Helper()
	deadline := time.After(timeout)
	for !flag.Load() {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", desc)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
