package spawncore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
)

// waitReal runs the real binary as a child and waits, returning its
// exit code. Used by span mode on every platform and as the only
// dispatch on Windows (no exec(2) there). Signals aimed at the shim
// are relayed to the child so Ctrl-C / kill behave as if the real
// tool had been launched directly; the relay set is per-OS
// (relaySignals).
func waitReal(real string, args []string) int {
	cmd := exec.Command(real, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "ycode-spawn: start %q: %v\n", real, err)
		return 1
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, relaySignals...)
	go func() {
		for s := range sigs {
			_ = cmd.Process.Signal(s)
		}
	}()

	err := cmd.Wait()
	signal.Stop(sigs)
	close(sigs)

	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "ycode-spawn: wait %q: %v\n", real, err)
	return 1
}
