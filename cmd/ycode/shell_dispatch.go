package main

import (
	"fmt"
	"os"
	"strings"
)

// maybeHandleShellCmd keeps bash-compatible -c parsing for wrapper invocations
// while routing the resulting command through the exact same Bashy contract as
// the Cobra command. Legacy shell modes and policy flags are rejected.
func maybeHandleShellCmd() bool {
	if len(os.Args) < 2 || (os.Args[1] != "shell" && os.Args[1] != "bash") {
		return false
	}
	flags, help, err := parseShellArgs(os.Args[2:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return true
	}
	if help {
		fmt.Fprintln(os.Stdout, "usage: ycode shell -c <command> [-f agent.yaml] [--workdir DIR] [--timeout DURATION]")
		return true
	}
	if err := runShellCmd(flags); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return true
}

func parseShellArgs(args []string) (*shellFlags, bool, error) {
	flags := &shellFlags{harnessFile: "agent.yaml"}
	help := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-h" || arg == "--help":
			help = true
		case arg == "-l" || arg == "--login":
			// Compatibility transport flag; it grants no capability.
		case arg == "-c" || arg == "--command":
			index++
			if index >= len(args) {
				return nil, false, errorsFlagValue(arg)
			}
			flags.command = args[index]
		case strings.HasPrefix(arg, "-c") && len(arg) > 2:
			flags.command = arg[2:]
		case arg == "-f" || arg == "--file":
			index++
			if index >= len(args) {
				return nil, false, errorsFlagValue(arg)
			}
			flags.harnessFile = args[index]
		case arg == "--workdir" || arg == "--timeout":
			index++
			if index >= len(args) {
				return nil, false, errorsFlagValue(arg)
			}
			if arg == "--workdir" {
				flags.workDir = args[index]
			} else {
				flags.timeoutString = args[index]
			}
		case arg == "--":
			if flags.command == "" && index+1 < len(args) {
				flags.command = args[index+1]
			}
			return flags, help, nil
		default:
			return nil, false, fmt.Errorf("shell: unsupported legacy argument %q", arg)
		}
	}
	return flags, help, nil
}

func errorsFlagValue(name string) error { return fmt.Errorf("shell: %s requires a value", name) }
