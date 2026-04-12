package cluster

import (
	"context"
	"time"
)

// Role indicates whether this instance is master or standby.
type Role int

const (
	RoleStandby Role = iota
	RoleMaster
)

func (r Role) String() string {
	if r == RoleMaster {
		return "master"
	}
	return "standby"
}

// Options configures cluster behavior.
type Options struct {
	// OnPromoted is called when this instance becomes master.
	OnPromoted func(ctx context.Context) error

	// OnDemoted is called when this instance stops being master.
	OnDemoted func(ctx context.Context) error

	// NATSPort is the port for the embedded NATS server. Default: 4222.
	NATSPort int

	// RetryInterval is how often standby instances attempt to acquire the lock.
	// Default: 3s.
	RetryInterval time.Duration

	// HeartbeatInterval is how often member files are updated. Default: 5s.
	HeartbeatInterval time.Duration

	// StaleThreshold is how old a heartbeat must be before cleanup. Default: 30s.
	StaleThreshold time.Duration

	// ForceMaster uses blocking flock (for ycode serve). Default: false.
	ForceMaster bool
}

func (o *Options) withDefaults() {
	if o.NATSPort == 0 {
		o.NATSPort = 4222
	}
	if o.RetryInterval == 0 {
		o.RetryInterval = defaultRetryInterval
	}
	if o.HeartbeatInterval == 0 {
		o.HeartbeatInterval = defaultHeartbeatInterval
	}
	if o.StaleThreshold == 0 {
		o.StaleThreshold = defaultStaleThreshold
	}
}
