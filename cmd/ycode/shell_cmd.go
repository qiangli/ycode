package main

import (
	"errors"
)

type shellFlags struct {
	harnessFile   string
	workDir       string
	command       string
	timeoutString string
	agentRef      string
}

func runShellCmd(flags *shellFlags) error {
	if flags.command == "" {
		return errors.New("shell requires -c; interactive execution is a configured repl or tui frontend")
	}
	return runHarnessShellOneShot(flags)
}
