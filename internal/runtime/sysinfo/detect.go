// Package sysinfo detects system capabilities for smart tool routing.
// All probes are non-fatal — a failed probe returns a zero/false value,
// never an error that blocks startup.
package sysinfo

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemContext holds detected system capabilities collected at startup.
type SystemContext struct {
	// Identity
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`         // runtime.GOOS
	Arch      string `json:"arch"`       // runtime.GOARCH
	OSVersion string `json:"os_version"` // e.g., "15.4.1" (macOS), "22.04" (Ubuntu)

	// Environment type
	IsContainer   bool   `json:"is_container"`   // running inside Docker/Podman/LXC
	ContainerRT   string `json:"container_rt"`   // "docker", "podman", "lxc", ""
	IsPrivileged  bool   `json:"is_privileged"`  // container has CAP_SYS_ADMIN
	IsWSL         bool   `json:"is_wsl"`         // Windows Subsystem for Linux
	IsCloud       bool   `json:"is_cloud"`       // cloud metadata endpoint reachable
	CloudProvider string `json:"cloud_provider"` // "aws", "gcp", "azure", ""
	IsCI          bool   `json:"is_ci"`          // CI environment detected

	// Capabilities
	HasInternet      bool   `json:"has_internet"`       // DNS/TCP connectivity
	CanRunContainers bool   `json:"can_run_containers"` // podman available (embedded or nested privileged)
	HasGit           bool   `json:"has_git"`            // git in PATH
	ShellPath        string `json:"shell_path"`         // $SHELL value

	// Resource hints
	NumCPU   int `json:"num_cpu"`
	MemoryMB int `json:"memory_mb"` // total system memory
}

// Detect probes the system and returns a SystemContext.
// All probes run concurrently with a 3s overall deadline.
func Detect() *SystemContext {
	ctx := &SystemContext{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		NumCPU: runtime.NumCPU(),
	}

	var wg sync.WaitGroup
	probe := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	probe(func() { ctx.Hostname, _ = os.Hostname() })
	probe(func() { ctx.OSVersion = detectOSVersion() })
	probe(func() { ctx.IsContainer, ctx.ContainerRT = detectContainer() })
	probe(func() { ctx.IsPrivileged = detectPrivileged() })
	probe(func() { ctx.IsWSL = detectWSL() })
	probe(func() { ctx.IsCloud, ctx.CloudProvider = detectCloud() })
	probe(func() { ctx.IsCI = detectCI() })
	probe(func() { ctx.HasInternet = detectInternet() })
	probe(func() { ctx.HasGit = detectBinary("git") })
	probe(func() { ctx.MemoryMB = detectMemory() })
	probe(func() { ctx.ShellPath = os.Getenv("SHELL") })

	wg.Wait()

	// Derive CanRunContainers from container + privilege state.
	// ycode has embedded podman — containers work unless we're inside
	// an unprivileged container that restricts nesting.
	ctx.CanRunContainers = !ctx.IsContainer || ctx.IsPrivileged

	return ctx
}

// Summary returns a human-readable summary of the system context.
func (s *SystemContext) Summary() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("%s/%s", s.OS, s.Arch))
	if s.OSVersion != "" {
		parts = append(parts, "v"+s.OSVersion)
	}

	// Environment type.
	switch {
	case s.IsCI:
		parts = append(parts, "CI")
	case s.IsContainer:
		env := "container"
		if s.ContainerRT != "" {
			env += "(" + s.ContainerRT + ")"
		}
		if s.IsPrivileged {
			env += "/privileged"
		}
		parts = append(parts, env)
	case s.IsWSL:
		parts = append(parts, "WSL")
	case s.IsCloud:
		env := "cloud"
		if s.CloudProvider != "" {
			env += "(" + s.CloudProvider + ")"
		}
		parts = append(parts, env)
	}

	// Capabilities.
	if !s.HasInternet {
		parts = append(parts, "air-gapped")
	}
	if !s.CanRunContainers {
		parts = append(parts, "no-containers")
	}

	return strings.Join(parts, ", ")
}

// --- Detection functions ---

func detectOSVersion() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	case "linux":
		f, err := os.Open("/etc/os-release")
		if err != nil {
			return ""
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "VERSION_ID=") {
				v := strings.TrimPrefix(line, "VERSION_ID=")
				return strings.Trim(v, `"`)
			}
		}
	case "windows":
		out, err := exec.Command("cmd", "/c", "ver").Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

func detectContainer() (bool, string) {
	// Check /.dockerenv (Docker creates this file).
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true, "docker"
	}

	// Check /run/.containerenv (Podman creates this).
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true, "podman"
	}

	// Parse cgroup for container runtime hints.
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false, ""
	}
	content := string(data)
	switch {
	case strings.Contains(content, "docker"):
		return true, "docker"
	case strings.Contains(content, "podman"):
		return true, "podman"
	case strings.Contains(content, "lxc"):
		return true, "lxc"
	}

	// Check for container-specific environment variables.
	if os.Getenv("container") != "" {
		return true, os.Getenv("container")
	}

	return false, ""
}

func detectPrivileged() bool {
	// Only relevant inside containers, but safe to check anywhere.
	// Parse /proc/self/status for CapEff (effective capabilities).
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CapEff:") {
			hexStr := strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
			caps, err := strconv.ParseUint(hexStr, 16, 64)
			if err != nil {
				return false
			}
			// CAP_SYS_ADMIN is bit 21.
			return caps&(1<<21) != 0
		}
	}
	return false
}

func detectWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

func detectCloud() (bool, string) {
	// AWS/GCP/Azure all use the link-local metadata endpoint.
	client := &http.Client{Timeout: 2 * time.Second}

	// Try AWS first (most common).
	resp, err := client.Get("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return true, "aws"
		}
	}

	// GCP uses a different header requirement but same IP.
	req, err := http.NewRequest("GET", "http://169.254.169.254/computeMetadata/v1/", nil)
	if err == nil {
		req.Header.Set("Metadata-Flavor", "Google")
		resp, err = client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true, "gcp"
			}
		}
	}

	// Azure uses a different path.
	req, err = http.NewRequest("GET", "http://169.254.169.254/metadata/instance?api-version=2021-02-01", nil)
	if err == nil {
		req.Header.Set("Metadata", "true")
		resp, err = client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true, "azure"
			}
		}
	}

	return false, ""
}

func detectInternet() bool {
	conn, err := net.DialTimeout("tcp", "dns.google:53", 2*time.Second)
	if err != nil {
		// Fallback: try Cloudflare DNS.
		conn, err = net.DialTimeout("tcp", "1.1.1.1:53", 2*time.Second)
		if err != nil {
			return false
		}
	}
	conn.Close()
	return true
}

func detectCI() bool {
	ciVars := []string{
		"CI", "GITHUB_ACTIONS", "JENKINS_URL", "GITLAB_CI",
		"CIRCLECI", "TRAVIS", "BUILDKITE", "TF_BUILD",
		"CODEBUILD_BUILD_ID", "TEAMCITY_VERSION",
	}
	for _, v := range ciVars {
		if os.Getenv(v) != "" {
			return true
		}
	}
	return false
}

func detectBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func detectMemory() int {
	switch runtime.GOOS {
	case "linux":
		f, err := os.Open("/proc/meminfo")
		if err != nil {
			return 0
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemTotal:") {
				// Format: "MemTotal:       16384000 kB"
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					kb, err := strconv.Atoi(fields[1])
					if err == nil {
						return kb / 1024
					}
				}
			}
		}
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err == nil {
			bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
			if err == nil {
				return int(bytes / (1024 * 1024))
			}
		}
	}
	return 0
}
