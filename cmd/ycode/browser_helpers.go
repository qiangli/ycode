package main

import (
	"os/exec"
)

// runDetached starts a command without waiting for it. Used to open
// URLs in the user's default browser via OS-specific tools.
func runDetached(name string, args []string) error {
	c := exec.Command(name, args...)
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}
