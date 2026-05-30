package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/container"
	ociMachine "github.com/qiangli/ycode/pkg/oci/machine"
)

// newPodmanMachineCmd wires user-facing machine lifecycle commands.
// Mirrors upstream `podman machine` UX so anyone familiar with podman
// can drop in.
func newPodmanMachineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "machine",
		Short: "Manage the embedded podman VM",
		Long: `Manage the Linux VM that hosts containers on macOS / Windows.

Most users never run these directly — ycode auto-provisions a default
machine on first container use. Use these subcommands to recover from
corrupted state (e.g. an orphan config without a registered
connection) or to inspect / tune the VM by hand.`,
	}
	cmd.AddCommand(
		newPodmanMachineInitCmd(),
		newPodmanMachineStartCmd(),
		newPodmanMachineStopCmd(),
		newPodmanMachineListCmd(),
		newPodmanMachineRmCmd(),
		newPodmanMachineResetCmd(),
	)
	return cmd
}

func newPodmanMachineInitCmd() *cobra.Command {
	cfg := container.DefaultMachineConfig()
	cmd := &cobra.Command{
		Use:   "init [NAME]",
		Short: "Create (and register) a new VM",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				cfg.Name = args[0]
			}
			if err := container.InitMachine(cmd.Context(), cfg); err != nil {
				return err
			}
			fmt.Printf("Machine %q initialized. Start it with: ycode podman machine start %s\n", cfg.Name, cfg.Name)
			return nil
		},
	}
	cmd.Flags().IntVar(&cfg.CPUs, "cpus", cfg.CPUs, "Number of vCPUs")
	cmd.Flags().IntVar(&cfg.Memory, "memory", cfg.Memory, "Memory in MB")
	cmd.Flags().IntVar(&cfg.Disk, "disk-size", cfg.Disk, "Disk size in GB")
	cmd.Flags().BoolVar(&cfg.NoAutoCleanup, "no-auto-cleanup", false,
		"Skip auto-cleanup of orphaned vfkit/gvproxy processes on preflight refusal")
	return cmd
}

func newPodmanMachineStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start [NAME]",
		Short: "Start a stopped VM",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := container.DefaultMachineConfig().Name
			if len(args) == 1 {
				name = args[0]
			}
			if err := container.StartMachine(cmd.Context(), name); err != nil {
				return err
			}
			fmt.Printf("Machine %q started\n", name)
			return nil
		},
	}
}

func newPodmanMachineStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [NAME]",
		Short: "Stop a running VM",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := container.DefaultMachineConfig().Name
			if len(args) == 1 {
				name = args[0]
			}
			if err := container.StopMachine(cmd.Context(), name); err != nil {
				return err
			}
			fmt.Printf("Machine %q stopped\n", name)
			return nil
		},
	}
}

func newPodmanMachineListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List managed VMs",
		RunE: func(cmd *cobra.Command, args []string) error {
			machines, err := container.ListMachines(cmd.Context())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tVMTYPE\tCPUS\tMEMORY\tDISK\tRUNNING")
			for _, m := range machines {
				fmt.Fprintf(w, "%s\t%s\t%d\t%d MiB\t%d GiB\t%t\n",
					m.Name, m.VMType, m.CPUs, uint64(m.Memory), uint64(m.DiskSize), m.Running)
			}
			w.Flush()
			return nil
		},
	}
}

func newPodmanMachineRmCmd() *cobra.Command {
	var (
		force        bool
		saveImage    bool
		saveIgnition bool
	)
	cmd := &cobra.Command{
		Use:   "rm [NAME]",
		Short: "Remove a VM",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := container.DefaultMachineConfig().Name
			if len(args) == 1 {
				name = args[0]
			}
			opts := ociMachine.RemoveOptions{
				Force:        force,
				SaveImage:    saveImage,
				SaveIgnition: saveIgnition,
			}
			if err := container.RemoveMachine(cmd.Context(), name, opts); err != nil {
				return err
			}
			fmt.Printf("Machine %q removed\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Stop and remove a running machine")
	cmd.Flags().BoolVar(&saveImage, "save-image", false, "Keep the downloaded VM disk image")
	cmd.Flags().BoolVar(&saveIgnition, "save-ignition", false, "Keep the ignition file")
	return cmd
}

func newPodmanMachineResetCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Wipe ALL machines and their state (recovery escape hatch)",
		Long: `Removes every machine config, image, ignition file, SSH key, and
registered podman connection on this host — across every provider.

Useful when ` + "`ycode podman version`" + ` fails with
` + `"connection \"X\" not found"` + ` because a previous init left an
orphan config on disk without registering the connection.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("refusing to reset without --yes (this is destructive)")
			}
			if err := container.ResetMachines(cmd.Context()); err != nil {
				return err
			}
			fmt.Println("All machines reset.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm the reset (required)")
	return cmd
}
