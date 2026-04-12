package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/cluster"
	"github.com/qiangli/ycode/internal/observability"
)

// joinCluster registers this instance in the cluster and starts the election
// loop. If this instance wins the election, it starts both the embedded NATS
// server and the OTEL observability stack.
func joinCluster(ctx context.Context) (*cluster.Cluster, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("user home dir: %w", err)
	}
	clusterDir := filepath.Join(home, ".ycode", "cluster")

	instanceID := fmt.Sprintf("otel-%d", os.Getpid())

	var stackMgr *observability.StackManager

	cl := cluster.New(clusterDir, instanceID, cluster.Options{
		NATSPort: otelPort + 100, // e.g., 58080 → 58180
		OnPromoted: func(ctx context.Context) error {
			cfg, dataDir, err := loadServeConfig()
			if err != nil {
				return err
			}
			cfg.ProxyPort = otelPort
			mgr := buildStackManager(cfg, dataDir)
			if err := mgr.Start(ctx); err != nil {
				return err
			}
			stackMgr = mgr
			slog.Info("cluster: OTEL stack started (this instance is master)", "port", otelPort)
			return nil
		},
		OnDemoted: func(ctx context.Context) error {
			if stackMgr != nil {
				slog.Info("cluster: stopping OTEL stack (demoted)")
				err := stackMgr.Stop(ctx)
				stackMgr = nil
				return err
			}
			return nil
		},
	})

	if err := cl.Join(ctx); err != nil {
		return nil, fmt.Errorf("cluster join: %w", err)
	}

	return cl, nil
}

// leaveCluster gracefully exits the cluster.
func leaveCluster(cl *cluster.Cluster) {
	slog.Info("cluster: leaving")
	if err := cl.Leave(context.Background()); err != nil {
		slog.Warn("cluster: leave", "error", err)
	}
}
