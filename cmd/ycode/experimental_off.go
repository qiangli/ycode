//go:build !experimental

package main

import (
	"context"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/projects"
)

// addExperimentalCmds is a no-op in stable builds. The experimental
// build tag adds backlog and foreman subcommands.
func addExperimentalCmds(_ *cobra.Command) {}

// startBacklogReconciler is a no-op in stable builds.
func startBacklogReconciler(_ context.Context, _ *slog.Logger, _ string, _ *gitserver.Client, _ *projects.Project) error {
	return nil
}
