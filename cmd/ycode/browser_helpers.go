package main

import (
	"fmt"
	"os/exec"
	"runtime"
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

// openInFileManager pops a Finder / file-manager window at the given
// path. Works for hidden directories on macOS (`open` accepts paths
// regardless of whether they would be visible in Finder browsing).
// Returns an error when no platform-appropriate command is found or
// fails to launch.
func openInFileManager(path string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{path}
	case "linux":
		name, args = "xdg-open", []string{path}
	case "windows":
		name, args = "explorer", []string{path}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return runDetached(name, args)
}
