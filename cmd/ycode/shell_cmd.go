package main

import (
	"errors"

	"github.com/spf13/cobra"
)

type shellFlags struct {
	harnessFile   string
	workDir       string
	command       string
	timeoutString string
}

func newShellCmd() *cobra.Command {
	f := &shellFlags{harnessFile: "agent.yaml"}
	cmd := &cobra.Command{
		Use:     "shell",
		Aliases: []string{"bash"},
		Short:   "Execute one command through the compiled Bashy contract",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runShellCmd(f)
		},
	}
	cmd.Flags().StringVarP(&f.command, "command", "c", "", "command to execute")
	cmd.Flags().StringVarP(&f.harnessFile, "file", "f", "agent.yaml", "strict harness configuration")
	cmd.Flags().StringVar(&f.workDir, "workdir", "", "working directory within the compiled workspace")
	cmd.Flags().StringVar(&f.timeoutString, "timeout", "", "execution timeout bounded by agent.yaml")
	return cmd
}

func runShellCmd(flags *shellFlags) error {
	if flags.command == "" {
		return errors.New("shell requires -c; interactive execution is a configured repl or tui frontend")
	}
	return runHarnessShellOneShot(flags)
}
