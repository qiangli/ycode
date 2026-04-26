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

			psArgs := []string{"ps", "--format=json"}
			if all {
				psArgs = append(psArgs, "-a")
			}

			out, err := engine.Run(cmd.Context(), psArgs...)
			if err != nil {
				return err
			}

			var containers []map[string]any
			if err := json.Unmarshal(out, &containers); err != nil {
				fmt.Print(string(out))
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "CONTAINER ID\tIMAGE\tSTATUS\tNAMES")
			for _, c := range containers {
				id := truncStr(fmt.Sprint(c["Id"]), 12)
				image := fmt.Sprint(c["Image"])
				status := fmt.Sprint(c["Status"])
				names := ""
				if n, ok := c["Names"]; ok {
					switch v := n.(type) {
					case []any:
						parts := make([]string, len(v))
						for i, p := range v {
							parts[i] = fmt.Sprint(p)
						}
						names = strings.Join(parts, ",")
					case string:
						names = v
					default:
						names = fmt.Sprint(v)
					}
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, image, status, names)
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

			out, err := engine.Run(cmd.Context(), "images", "--format=json")
			if err != nil {
				return err
			}

			var images []map[string]any
			if err := json.Unmarshal(out, &images); err != nil {
				fmt.Print(string(out))
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "REPOSITORY\tTAG\tIMAGE ID\tSIZE")
			for _, img := range images {
				repo := ""
				tag := ""
				if names, ok := img["Names"]; ok {
					switch v := names.(type) {
					case []any:
						if len(v) > 0 {
							parts := strings.SplitN(fmt.Sprint(v[0]), ":", 2)
							repo = parts[0]
							if len(parts) > 1 {
								tag = parts[1]
							}
						}
					}
				}
				if repo == "" {
					repo = "<none>"
					tag = "<none>"
				}
				id := truncStr(fmt.Sprint(img["Id"]), 12)
				size := formatSize(img["Size"])
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", repo, tag, id, size)
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
			return engine.PullImage(cmd.Context(), args[0])
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
			execArgs := append([]string{"exec", args[0]}, args[1:]...)
			out, err := engine.Run(cmd.Context(), execArgs...)
			if out != nil {
				fmt.Print(string(out))
			}
			return err
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
			logsArgs := []string{"logs"}
			if follow {
				logsArgs = append(logsArgs, "-f")
			}
			if tail != "" {
				logsArgs = append(logsArgs, "--tail", tail)
			}
			logsArgs = append(logsArgs, args[0])
			out, err := engine.Run(cmd.Context(), logsArgs...)
			if out != nil {
				fmt.Print(string(out))
			}
			return err
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
			stopArgs := append([]string{"stop"}, args...)
			out, err := engine.Run(cmd.Context(), stopArgs...)
			if out != nil {
				fmt.Print(string(out))
			}
			return err
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
			rmArgs := []string{"rm"}
			if force {
				rmArgs = append(rmArgs, "-f")
			}
			rmArgs = append(rmArgs, args...)
			out, err := engine.Run(cmd.Context(), rmArgs...)
			if out != nil {
				fmt.Print(string(out))
			}
			return err
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
			runArgs := []string{"run"}
			if rm {
				runArgs = append(runArgs, "--rm")
			}
			if detach {
				runArgs = append(runArgs, "-d")
			}
			runArgs = append(runArgs, args...)
			out, err := engine.Run(cmd.Context(), runArgs...)
			if out != nil {
				fmt.Print(string(out))
			}
			return err
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
			// Pretty-print JSON.
			var pretty json.RawMessage
			if err := json.Unmarshal(data, &pretty); err == nil {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				enc.Encode(pretty)
			} else {
				fmt.Println(string(data))
			}
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

func formatSize(v any) string {
	switch s := v.(type) {
	case float64:
		if s > 1e9 {
			return fmt.Sprintf("%.1f GB", s/1e9)
		}
		if s > 1e6 {
			return fmt.Sprintf("%.1f MB", s/1e6)
		}
		return fmt.Sprintf("%.0f B", s)
	default:
		return fmt.Sprint(v)
	}
}
