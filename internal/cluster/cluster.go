//go:build unix

package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
)

// Cluster coordinates multiple ycode instances via flock + embedded NATS.
type Cluster struct {
	baseDir    string
	instanceID string
	opts       Options

	elect  *election
	member *memberManager

	role atomic.Int32 // current Role

	mu sync.Mutex
	ns *natsServer // non-nil if this instance hosts the NATS server
	nc *nats.Conn  // NATS client connection

	cancel context.CancelFunc
	done   chan struct{}
}

// New creates a Cluster instance. Call Join to start participating.
func New(baseDir, instanceID string, opts Options) *Cluster {
	opts.withDefaults()
	return &Cluster{
		baseDir:    baseDir,
		instanceID: instanceID,
		opts:       opts,
		elect:      newElection(baseDir),
		member:     newMemberManager(baseDir, instanceID),
		done:       make(chan struct{}),
	}
}

// Join registers as a member and starts election + heartbeat goroutines.
// Returns immediately (non-blocking).
func (c *Cluster) Join(ctx context.Context) error {
	if err := c.member.register(RoleStandby); err != nil {
		return fmt.Errorf("register member: %w", err)
	}

	ctx, c.cancel = context.WithCancel(ctx)

	go c.run(ctx)

	return nil
}

// Leave gracefully exits the cluster. Blocks until shutdown is complete.
func (c *Cluster) Leave(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}

	// Wait for the run loop to finish.
	select {
	case <-c.done:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// Role returns the current role of this instance.
func (c *Cluster) Role() Role {
	return Role(c.role.Load())
}

// Members returns info about all registered members.
func (c *Cluster) Members() ([]MemberInfo, error) {
	return c.member.listMembers()
}

// LeaderInfo returns the current leader's info from nats.json, or nil if unavailable.
func (c *Cluster) LeaderInfo() (*NATSInfo, error) {
	return readNATSInfo(filepath.Join(c.baseDir, "nats.json"))
}

// Conn returns the NATS client connection, or nil if not connected.
func (c *Cluster) Conn() *nats.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nc
}

// run is the main lifecycle goroutine.
func (c *Cluster) run(ctx context.Context) {
	defer close(c.done)
	defer c.cleanup(context.Background())

	if c.opts.ForceMaster {
		c.runForceMaster(ctx)
		return
	}
	c.runElectionLoop(ctx)
}

// runForceMaster acquires the lock with blocking and stays master until context is cancelled.
func (c *Cluster) runForceMaster(ctx context.Context) {
	if err := c.elect.acquireBlocking(); err != nil {
		slog.Warn("cluster: failed to acquire lock (blocking)", "error", err)
		return
	}

	if err := c.promote(ctx); err != nil {
		slog.Warn("cluster: promotion failed", "error", err)
		c.elect.release()
		return
	}

	// Hold leadership until context cancelled.
	c.runLeaderLoop(ctx)
}

// runElectionLoop tries to acquire the lock periodically.
func (c *Cluster) runElectionLoop(ctx context.Context) {
	for {
		acquired, err := c.elect.tryAcquire()
		if err != nil {
			slog.Warn("cluster: election error", "error", err)
		}

		if acquired {
			if err := c.promoteWithRetry(ctx); err != nil {
				slog.Warn("cluster: promotion failed, releasing lock", "error", err)
				c.elect.release()
			} else {
				c.runLeaderLoop(ctx)
				return
			}
		}

		// Try to connect to existing NATS server as client.
		c.tryConnectAsClient()

		select {
		case <-ctx.Done():
			return
		case <-time.After(c.opts.RetryInterval):
		}
	}
}

// runLeaderLoop runs heartbeat and stale cleanup while leader.
func (c *Cluster) runLeaderLoop(ctx context.Context) {
	heartbeat := time.NewTicker(c.opts.HeartbeatInterval)
	defer heartbeat.Stop()

	cleanup := time.NewTicker(c.opts.StaleThreshold)
	defer cleanup.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			c.member.writeInfo(RoleMaster)
		case <-cleanup.C:
			c.member.cleanStale(c.opts.StaleThreshold)
		}
	}
}

// promote starts the NATS server and calls OnPromoted.
func (c *Cluster) promote(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	infoPath := filepath.Join(c.baseDir, "nats.json")
	storeDir := filepath.Join(c.baseDir, "nats-data")

	ns, err := startNATSServer(c.opts.NATSPort, storeDir, infoPath)
	if err != nil {
		return fmt.Errorf("start nats server: %w", err)
	}

	// Update nats.json with our instance ID.
	info := NATSInfo{
		Host:       "127.0.0.1",
		Port:       c.opts.NATSPort,
		PID:        os.Getpid(),
		InstanceID: c.instanceID,
	}
	data, _ := json.Marshal(info)
	os.WriteFile(infoPath, data, 0o644)

	// Connect as local client.
	addr := fmt.Sprintf("nats://127.0.0.1:%d", c.opts.NATSPort)
	nc, err := connectNATS(addr, infoPath)
	if err != nil {
		ns.stop()
		return fmt.Errorf("connect to own nats: %w", err)
	}

	c.ns = ns
	c.nc = nc
	c.role.Store(int32(RoleMaster))
	c.member.writeInfo(RoleMaster)

	if c.opts.OnPromoted != nil {
		if err := c.opts.OnPromoted(ctx); err != nil {
			nc.Close()
			ns.stop()
			c.ns = nil
			c.nc = nil
			c.role.Store(int32(RoleStandby))
			return fmt.Errorf("on promoted: %w", err)
		}
	}

	slog.Info("cluster: promoted to master", "instanceID", c.instanceID)
	return nil
}

// promoteWithRetry tries promotion up to maxPromotionRetries times.
func (c *Cluster) promoteWithRetry(ctx context.Context) error {
	var lastErr error
	for i := range maxPromotionRetries {
		if err := c.promote(ctx); err != nil {
			lastErr = err
			slog.Warn("cluster: promotion attempt failed",
				"attempt", i+1, "max", maxPromotionRetries, "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(promotionRetryBackoff):
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("promotion failed after %d attempts: %w", maxPromotionRetries, lastErr)
}

// demote stops the NATS server and calls OnDemoted.
func (c *Cluster) demote(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.opts.OnDemoted != nil {
		if err := c.opts.OnDemoted(ctx); err != nil {
			slog.Warn("cluster: on demoted", "error", err)
		}
	}

	if c.nc != nil {
		c.nc.Close()
		c.nc = nil
	}
	if c.ns != nil {
		c.ns.stop()
		c.ns = nil
	}

	c.role.Store(int32(RoleStandby))
	slog.Info("cluster: demoted from master", "instanceID", c.instanceID)
}

// tryConnectAsClient attempts to connect to an existing NATS server as a standby client.
func (c *Cluster) tryConnectAsClient() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.nc != nil && c.nc.IsConnected() {
		return
	}

	infoPath := filepath.Join(c.baseDir, "nats.json")
	nc, err := connectNATS("", infoPath)
	if err != nil {
		return // no server available yet
	}

	if c.nc != nil {
		c.nc.Close()
	}
	c.nc = nc
}

// cleanup runs on shutdown: demote if master, deregister, release lock.
func (c *Cluster) cleanup(ctx context.Context) {
	if c.Role() == RoleMaster {
		c.demote(ctx)
	}

	c.mu.Lock()
	if c.nc != nil {
		c.nc.Close()
		c.nc = nil
	}
	c.mu.Unlock()

	c.member.deregister()
	c.elect.release()
}
