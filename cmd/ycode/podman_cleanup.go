package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/container"
)

func newPodmanCleanupCmd() *cobra.Command {
	var dryRun bool
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Remove orphaned vfkit/gvproxy processes and stale podman sockets",
		Long: `Removes host-side VM state that's been left behind by crashed or
abandoned machine lifecycles:

  - vfkit processes whose disk image is gone (machine was removed but
    vfkit didn't exit)
  - gvproxy processes whose pid-file is gone (same scenario)
  - *.sock files in the podman tmpdir that no live process references

All operations are OFFLINE — no podman socket required. This is the same
cleanup the preflight runs automatically when a machine init / start
fails on insufficient resources. Run it directly when you'd like to
preview state with --dry-run, or recover from a manual crash where
the auto-flow couldn't trigger (the machine was never asked to start).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := container.CleanupHost(container.HostCleanupOptions{DryRun: dryRun})
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(report)
			}
			return printCleanupReport(os.Stdout, report)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview what would be removed without doing it")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}

func printCleanupReport(out *os.File, r container.HostCleanupReport) error {
	verb := "removed"
	if r.DryRun {
		verb = "would remove"
	}
	if !r.AnythingCleaned() {
		fmt.Fprintln(out, "cleanup: nothing to remove (no orphaned vfkit/gvproxy, no stale sockets)")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if len(r.OrphanedProcesses) > 0 {
		fmt.Fprintf(tw, "\nOrphaned processes (%s):\n", verb)
		fmt.Fprintln(tw, "  PID\tCOMMAND\tREASON")
		for _, p := range r.OrphanedProcesses {
			fmt.Fprintf(tw, "  %d\t%s\t%s\n", p.PID, p.Command, p.Reason)
		}
	}
	if len(r.StaleSockets) > 0 {
		fmt.Fprintf(tw, "\nStale sockets (%s):\n", verb)
		fmt.Fprintln(tw, "  PATH\tREASON")
		for _, s := range r.StaleSockets {
			fmt.Fprintf(tw, "  %s\t%s\n", s.Path, s.Reason)
		}
	}
	return tw.Flush()
}
