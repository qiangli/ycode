package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
		newPodmanBuildCmd(),
		newPodmanNetworkCmd(),
		newPodmanMachineCmd(),
	)

	brandFlagErrors(cmd)

	return cmd
}

// brandFlagErrors prepends a "this is ycode's embedded podman" header to any
// unknown-flag / parse error so users running through the scripts/shims/podman
// shim realize they aren't talking to upstream podman. Without this, a missing
// flag like `-w` surfaces as a bare "unknown flag" line that's easy to
// misattribute to the real binary.
func brandFlagErrors(root *cobra.Command) {
	errFn := func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("ycode podman (embedded engine, not upstream podman): %w", err)
	}
	root.SetFlagErrorFunc(errFn)
	for _, sub := range root.Commands() {
		sub.SetFlagErrorFunc(errFn)
		for _, gsub := range sub.Commands() {
			gsub.SetFlagErrorFunc(errFn)
		}
	}
}

// newEngine creates a container engine for CLI use.
//
// The Engine retains the context it was created with as its connection
// context for every subsequent REST call (engine.go's connCtx). Using a
// WithTimeout context here would cap *all* operations — including long
// ones like `build` (~minutes) — at that timeout, so we hand it the
// process-lifetime context.Background() and let individual commands
// cancel via Ctrl-C / cmd.Context() if they need to.
func newEngine() (*container.Engine, error) {
	return container.NewEngine(context.Background(), &container.EngineConfig{})
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
	cmd := &cobra.Command{
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
	// Pass `sh -c "..."` and other flag-shaped command args through to
	// the container rather than letting pflag try to parse them.
	cmd.Flags().SetInterspersed(false)
	return cmd
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
	var (
		rm          bool
		detach      bool
		name        string
		network     string
		ports       []string
		volumes     []string
		envs        []string
		envFiles    []string
		privileged  bool
		capAdd      []string
		workdir     string
		interactive bool
		tty         bool
	)
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
				Image:   args[0],
				Name:    name,
				Network: network,
				WorkDir: workdir,
				// CLI `run` is for docker-compatible workloads (postgres,
				// redis, etc.) that expect to start with Linux caps. The
				// engine's "drop ALL by default" stance is the right pick
				// for the sandbox use case but breaks ordinary images, so
				// the CLI explicitly opts out of it. Users who want the
				// sandbox-grade isolation can pass --cap-drop ALL — wire
				// when --cap-drop flag is added.
				KeepCaps:   true,
				CapAdd:     capAdd,
				Privileged: privileged,
			}
			if (interactive || tty) && detach {
				fmt.Fprintln(os.Stderr, "ycode podman run: -i/-t are no-ops in detach mode")
			} else if interactive || tty {
				fmt.Fprintln(os.Stderr, "ycode podman run: -i/-t accepted but not yet wired to a PTY; container runs without an attached terminal")
			}
			if len(args) > 1 {
				cfg.Command = args[1:]
			}

			cfg.Ports, err = parsePortMappings(ports)
			if err != nil {
				return err
			}
			cfg.Mounts, err = parseMounts(volumes)
			if err != nil {
				return err
			}
			cfg.Env, err = collectEnv(envs, envFiles)
			if err != nil {
				return err
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
	cmd.Flags().StringVar(&name, "name", "", "Assign a name to the container")
	cmd.Flags().StringVar(&network, "network", "", "Connect a container to a network")
	cmd.Flags().StringSliceVarP(&ports, "publish", "p", nil, "Publish a container's port to the host (HOST:CTR[/PROTO])")
	cmd.Flags().StringSliceVarP(&volumes, "volume", "v", nil, "Bind mount a volume (HOST:CTR[:ro])")
	cmd.Flags().StringSliceVarP(&envs, "env", "e", nil, "Set environment variable (KEY=VALUE)")
	cmd.Flags().StringSliceVar(&envFiles, "env-file", nil, "Read environment variables from file (KEY=VALUE per line)")
	cmd.Flags().BoolVar(&privileged, "privileged", false, "Give extended privileges to the container (all caps, all devices, no seccomp/SELinux/AppArmor)")
	cmd.Flags().StringSliceVar(&capAdd, "cap-add", nil, "Add Linux capability (e.g. NET_ADMIN, SYS_ADMIN). Ignored when --privileged is set.")
	cmd.Flags().StringVarP(&workdir, "workdir", "w", "", "Working directory inside the container")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Keep STDIN open (accepted for docker/podman compatibility; not yet wired)")
	cmd.Flags().BoolVarP(&tty, "tty", "t", false, "Allocate a pseudo-TTY (accepted for docker/podman compatibility; not yet wired)")
	// Treat anything after the IMAGE positional as the command, so users
	// can write `podman run alpine sh -c "echo hi"` without pflag eating
	// the `-c` as if it were a run-level flag.
	cmd.Flags().SetInterspersed(false)
	return cmd
}

// --- build ---

func newPodmanBuildCmd() *cobra.Command {
	var (
		tag        string
		dockerfile string
		buildArgs  []string
	)
	cmd := &cobra.Command{
		Use:   "build [FLAGS] CONTEXT",
		Short: "Build an image from a Dockerfile",
		Long: `Build a container image using the project's existing Dockerfile.

CONTEXT is the build context directory (passed to COPY / ADD instructions).
Defaults to the current directory if omitted.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return fmt.Errorf("build: -t/--tag is required")
			}
			ctxDir := "."
			if len(args) == 1 {
				ctxDir = args[0]
			}
			absCtx, err := filepath.Abs(ctxDir)
			if err != nil {
				return fmt.Errorf("resolve context: %w", err)
			}
			dfPath := dockerfile
			if dfPath == "" {
				dfPath = filepath.Join(absCtx, "Dockerfile")
			} else if !filepath.IsAbs(dfPath) {
				dfPath = filepath.Join(absCtx, dfPath)
			}

			argMap, err := parseBuildArgs(buildArgs)
			if err != nil {
				return err
			}

			engine, err := newEngine()
			if err != nil {
				return err
			}
			if err := engine.BuildImageFromContext(cmd.Context(), tag, absCtx, dfPath, argMap); err != nil {
				return err
			}
			fmt.Printf("Successfully built %s\n", tag)
			return nil
		},
	}
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "Name and optionally a tag in the 'name:tag' format")
	cmd.Flags().StringVarP(&dockerfile, "file", "f", "", "Path to the Dockerfile (default: <CONTEXT>/Dockerfile)")
	cmd.Flags().StringSliceVar(&buildArgs, "build-arg", nil, "Set build-time variable (KEY=VALUE)")
	return cmd
}

// --- network ---

func newPodmanNetworkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Manage container networks",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "create NAME",
			Short: "Create a bridge network",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				engine, err := newEngine()
				if err != nil {
					return err
				}
				if err := engine.CreateNetwork(cmd.Context(), args[0]); err != nil {
					return err
				}
				fmt.Println(args[0])
				return nil
			},
		},
		func() *cobra.Command {
			var filter string
			c := &cobra.Command{
				Use:   "ls",
				Short: "List networks",
				RunE: func(cmd *cobra.Command, args []string) error {
					engine, err := newEngine()
					if err != nil {
						return err
					}
					nets, err := engine.ListNetworks(cmd.Context(), filter)
					if err != nil {
						return err
					}
					w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
					fmt.Fprintln(w, "NAME\tDRIVER")
					for _, n := range nets {
						fmt.Fprintf(w, "%s\t%s\n", n.Name, n.Driver)
					}
					w.Flush()
					return nil
				},
			}
			c.Flags().StringVar(&filter, "filter", "", "Substring match on network name")
			return c
		}(),
		&cobra.Command{
			Use:   "rm NAME [NAME...]",
			Short: "Remove one or more networks",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				engine, err := newEngine()
				if err != nil {
					return err
				}
				var firstErr error
				for _, n := range args {
					if err := engine.RemoveNetwork(cmd.Context(), n); err != nil {
						fmt.Fprintf(os.Stderr, "Error removing %s: %v\n", n, err)
						if firstErr == nil {
							firstErr = err
						}
						continue
					}
					fmt.Println(n)
				}
				return firstErr
			},
		},
	)
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

// --- flag parsers ---

// parsePortMappings reads docker-style "HOST:CTR[/PROTO]" port specs.
// CTR alone (no colon) maps the same port on both sides; a /udp suffix
// switches the protocol from the default tcp.
func parsePortMappings(specs []string) ([]container.PortMapping, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make([]container.PortMapping, 0, len(specs))
	for _, s := range specs {
		proto := "tcp"
		if i := strings.Index(s, "/"); i >= 0 {
			proto = s[i+1:]
			s = s[:i]
		}
		hostStr, ctrStr := s, s
		if i := strings.Index(s, ":"); i >= 0 {
			hostStr, ctrStr = s[:i], s[i+1:]
		}
		host, err := strconv.ParseUint(hostStr, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid host port %q: %w", hostStr, err)
		}
		ctr, err := strconv.ParseUint(ctrStr, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid container port %q: %w", ctrStr, err)
		}
		out = append(out, container.PortMapping{
			HostPort:      uint16(host),
			ContainerPort: uint16(ctr),
			Protocol:      proto,
		})
	}
	return out, nil
}

// parseMounts reads docker-style "HOST:CTR[:ro]" volume specs.
func parseMounts(specs []string) ([]container.Mount, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make([]container.Mount, 0, len(specs))
	for _, s := range specs {
		parts := strings.Split(s, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fmt.Errorf("invalid volume spec %q (want HOST:CTR[:ro])", s)
		}
		m := container.Mount{Source: parts[0], Target: parts[1]}
		if len(parts) == 3 {
			switch parts[2] {
			case "ro":
				m.ReadOnly = true
			case "rw":
				m.ReadOnly = false
			default:
				return nil, fmt.Errorf("invalid mount option %q (want ro or rw)", parts[2])
			}
		}
		if !filepath.IsAbs(m.Source) {
			abs, err := filepath.Abs(m.Source)
			if err != nil {
				return nil, fmt.Errorf("resolve mount source %q: %w", m.Source, err)
			}
			m.Source = abs
		}
		out = append(out, m)
	}
	return out, nil
}

// collectEnv merges -e and --env-file values into a single map. -e wins
// on collision (matches docker / podman semantics).
func collectEnv(envs, envFiles []string) (map[string]string, error) {
	if len(envs) == 0 && len(envFiles) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, f := range envFiles {
		if err := readEnvFile(f, out); err != nil {
			return nil, err
		}
	}
	for _, kv := range envs {
		k, v, ok := splitKV(kv)
		if !ok {
			return nil, fmt.Errorf("invalid -e value %q (want KEY=VALUE)", kv)
		}
		out[k] = v
	}
	return out, nil
}

// readEnvFile parses a KEY=VALUE-per-line file, skipping blanks and #-comments.
// Quotes around the value are not unwrapped (matches docker behavior — the
// quotes become part of the value).
func readEnvFile(path string, out map[string]string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open env-file %s: %w", path, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := splitKV(line)
		if !ok {
			return fmt.Errorf("env-file %s: malformed line %q", path, line)
		}
		out[k] = v
	}
	return scanner.Err()
}

func splitKV(s string) (string, string, bool) {
	i := strings.Index(s, "=")
	if i <= 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

func parseBuildArgs(specs []string) (map[string]string, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, kv := range specs {
		k, v, ok := splitKV(kv)
		if !ok {
			return nil, fmt.Errorf("invalid --build-arg %q (want KEY=VALUE)", kv)
		}
		out[k] = v
	}
	return out, nil
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
