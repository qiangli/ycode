package sandbox

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Detection identifies the execution environment and available container runtimes.
type Detection struct {
	InContainer     bool
	InDocker        bool
	Platform        string
	PodmanAvailable bool   // podman binary found in PATH
	PodmanPath      string // path to podman binary
}

// Detect checks the current execution environment.
func Detect() *Detection {
	d := &Detection{
		Platform: runtime.GOOS,
	}

	// Check for container indicators.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		d.InDocker = true
		d.InContainer = true
	}

	// Check cgroup.
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") || strings.Contains(content, "containerd") {
			d.InContainer = true
		}
	}

	// Check for podman runtime availability.
	if path, err := exec.LookPath("podman"); err == nil {
		d.PodmanAvailable = true
		d.PodmanPath = path
	}

	return d
}

// IsLinux returns true if running on Linux.
func IsLinux() bool {
	return runtime.GOOS == "linux"
}

// IsMacOS returns true if running on macOS.
func IsMacOS() bool {
	return runtime.GOOS == "darwin"
}
