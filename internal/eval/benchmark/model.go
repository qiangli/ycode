package benchmark

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// SelectModel picks the best Ollama model that fits in available RAM.
func SelectModel(ramGB int) string {
	switch {
	case ramGB >= 32:
		return "qwen2.5-coder:32b"
	case ramGB >= 24:
		return "qwen2.5-coder:14b"
	case ramGB >= 16:
		return "qwen2.5-coder:7b"
	default:
		return "qwen2.5-coder:3b"
	}
}

// DetectHostRAM returns total system RAM in GB.
func DetectHostRAM() (int, error) {
	switch runtime.GOOS {
	case "darwin":
		return detectRAMDarwin()
	case "linux":
		return detectRAMLinux()
	default:
		return 16, nil // safe default
	}
}

func detectRAMDarwin() (int, error) {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, fmt.Errorf("sysctl hw.memsize: %w", err)
	}
	bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse hw.memsize: %w", err)
	}
	return int(bytes / (1024 * 1024 * 1024)), nil
}

func detectRAMLinux() (int, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err != nil {
					return 0, err
				}
				return int(kb / (1024 * 1024)), nil
			}
		}
	}
	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}
