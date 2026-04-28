package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/container"
)

func newPodmanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "podman",
		Aliases: []string{"docker"},
		Short:   "Container management (Podman-based)",
		Long:    "Manage containers, images, and networks via the embedded Podman engine.",
	}

	cmd.AddCommand(
		newPodmanPsCmd(),
		newPodmanImagesCmd(),
		newPodmanPullCmd(),
		newPodmanExecCmd(),
		newPodmanLogsCmd(),
		newPodmanStopCmd(),
		newPodmanRmCmd(),
		newPodmanRunCmd(),
		newPodmanVersionCmd(),
		newPodmanInspectCmd(),
	)

	return cmd
}

// newEngine creates a container engine for CLI use.
func newEngine() (*container.Engine, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return container.NewEngine(ctx, &container.EngineConfig{})
}

// --- ps ---

func newPodmanPsCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newEngine()
			if err != nil {
				return err
			}

			var filters map[string]string
			if !all {
				filters = map[string]string{"status": "running"}
			}

			containers, err := engine.ListContainers(cmd.Context(), filters)
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "CONTAINER ID\tIMAGE\tSTATUS\tNAMES")
			for _, c := range containers {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					truncStr(c.ID, 12), c.Image, c.State, c.Name)
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show all containers (default shows just running)")
	return cmd
}

// --- images ---

func newPodmanImagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "images",
		Short: "List images",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newEngine()
			if err != nil {
				return err
			}

			images, err := engine.ListImages(cmd.Context())
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "REPOSITORY\tTAG\tIMAGE ID\tSIZE")
			for _, img := range images {
				repo := "<none>"
				tag := "<none>"
				if len(img.Names) > 0 {
					parts := strings.SplitN(img.Names[0], ":", 2)
					repo = parts[0]
					if len(parts) > 1 {
						tag = parts[1]
					}
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					repo, tag, truncStr(img.ID, 12), formatSize(float64(img.Size)))
			}
			w.Flush()
			return nil
		},
	}
}

// --- pull ---

func newPodmanPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull IMAGE",
		Short: "Pull an image from a registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newEngine()
			if err != nil {
				return err
			}
			if err := engine.PullImage(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Pulled %s\n", args[0])
			return nil
		},
	}
}

// --- exec ---

func newPodmanExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec CONTAINER COMMAND [ARG...]",
		Short: "Execute a command in a running container",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newEngine()
			if err != nil {
				return err
			}
			ctr := container.NewContainer(engine, args[0])
			command := strings.Join(args[1:], " ")
			result, err := ctr.Exec(cmd.Context(), command, "")
			if err != nil {
				return err
			}
			if result.Stdout != "" {
				fmt.Println(result.Stdout)
			}
			if result.Stderr != "" {
				fmt.Fprintln(os.Stderr, result.Stderr)
			}
			if result.ExitCode != 0 {
				os.Exit(result.ExitCode)
			}
			return nil
		},
	}
}

// --- logs ---

func newPodmanLogsCmd() *cobra.Command {
	var follow bool
	var tail string
	cmd := &cobra.Command{
		Use:   "logs CONTAINER",
		Short: "Fetch the logs of a container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newEngine()
			if err != nil {
				return err
			}
			logs, err := engine.ContainerLogs(cmd.Context(), args[0], follow, tail)
			if err != nil {
				return err
			}
			fmt.Print(logs)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().StringVar(&tail, "tail", "", "Number of lines to show from the end of the logs")
	return cmd
}

// --- stop ---

func newPodmanStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop CONTAINER [CONTAINER...]",
		Short: "Stop one or more running containers",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newEngine()
			if err != nil {
				return err
			}
			for _, id := range args {
				ctr := container.NewContainer(engine, id)
				if err := ctr.Stop(cmd.Context(), 10*time.Second); err != nil {
					fmt.Fprintf(os.Stderr, "Error stopping %s: %v\n", id, err)
				} else {
					fmt.Println(id)
				}
			}
			return nil
		},
	}
}

// --- rm ---

func newPodmanRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm CONTAINER [CONTAINER...]",
		Short: "Remove one or more containers",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newEngine()
			if err != nil {
				return err
			}
			for _, id := range args {
				ctr := container.NewContainer(engine, id)
				if err := ctr.Remove(cmd.Context(), force); err != nil {
					fmt.Fprintf(os.Stderr, "Error removing %s: %v\n", id, err)
				} else {
					fmt.Println(id)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force removal of a running container")
	return cmd
}

// --- run ---

func newPodmanRunCmd() *cobra.Command {
	var rm bool
	var detach bool
	cmd := &cobra.Command{
		Use:   "run [FLAGS] IMAGE [COMMAND] [ARG...]",
		Short: "Run a command in a new container",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newEngine()
			if err != nil {
				return err
			}

			cfg := &container.ContainerConfig{
				Image: args[0],
			}
			if len(args) > 1 {
				cfg.Command = args[1:]
			}

			ctr, err := engine.CreateContainer(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			if rm {
				defer ctr.Remove(cmd.Context(), true)
			}

			if err := ctr.Start(cmd.Context()); err != nil {
				return err
			}

			if detach {
				fmt.Println(ctr.ID)
				return nil
			}

			// Wait for container to finish and print logs.
			logs, err := engine.ContainerLogs(cmd.Context(), ctr.ID, false, "")
			if err == nil && logs != "" {
				fmt.Print(logs)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&rm, "rm", false, "Automatically remove the container when it exits")
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, "Run container in background")
	return cmd
}

// --- version ---

func newPodmanVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Display the Podman version",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newEngine()
			if err != nil {
				return err
			}
			v, err := engine.Version(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Printf("podman version %s\n", v)
			return nil
		},
	}
}

// --- inspect ---

func newPodmanInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect CONTAINER",
		Short: "Display detailed information on a container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newEngine()
			if err != nil {
				return err
			}
			data, err := engine.InspectContainer(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(json.RawMessage(data))
			return nil
		},
	}
}

// --- helpers ---

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func formatSize(s float64) string {
	if s > 1e9 {
		return fmt.Sprintf("%.1f GB", s/1e9)
	}
	if s > 1e6 {
		return fmt.Sprintf("%.1f MB", s/1e6)
	}
	return fmt.Sprintf("%.0f B", s)
}
