// Package container — host-resource preflight for VM provisioning.
//
// Why this exists: ycode podman runs Linux containers inside a vfkit/
// libkrun VM on macOS (and a native daemon on Linux). The VM has a
// fixed memory + disk budget set at `machine init` time. If a user
// requests a 4 GB VM on a machine that's already swapping, or a
// 50 GB sparse disk image on a partition with 10 GB free, the result
// isn't a clean failure — it's OOM-killed builds, IO stalls, and
// (worst case) corrupted disk images. We hit exactly this scenario
// during cloudbox+k3s build testing, where the 4GB VM OOM'd silently
// and left vfkit + gvproxy zombie processes behind.
//
// The preflight runs before any state mutation (no vfkit spawn, no
// disk-image allocation) and returns a clear error with what's
// available + suggested mitigation. Callers compose it into
// `machine init` and `machine start`.

package container

import (
	"fmt"
	"strconv"
	"strings"
)

// parseVMStatFreeMB extracts (free + inactive + speculative + purgeable)
// from `vm_stat` output, multiplied by the page size announced in the
// header line. Pure string parser so it tests on every platform (the
// I/O-side caller in preflight_darwin.go is darwin-only).
func parseVMStatFreeMB(out string) (uint64, error) {
	pageSize := uint64(16384) // default for arm64 macOS; pre-Apple-Silicon was 4096
	var free, inactive, speculative, purgeable uint64
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Mach Virtual Memory Statistics") {
			// "...Statistics: (page size of 16384 bytes)"
			i := strings.Index(line, "page size of ")
			if i < 0 {
				continue
			}
			rest := line[i+len("page size of "):]
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				if v, perr := strconv.ParseUint(fields[0], 10, 64); perr == nil {
					pageSize = v
				}
			}
			continue
		}
		// "Pages <category>: <count>."
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimRight(strings.TrimSpace(parts[1]), ".")
		pages, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "Pages free":
			free = pages
		case "Pages inactive":
			inactive = pages
		case "Pages speculative":
			speculative = pages
		case "Pages purgeable":
			purgeable = pages
		}
	}
	totalAvailablePages := free + inactive + speculative + purgeable
	return (totalAvailablePages * pageSize) / (1024 * 1024), nil
}

// parseMeminfo extracts MemAvailable + MemTotal from a /proc/meminfo
// dump. Values are in kB per kernel convention; this returns MB.
// Pure string parser; the I/O-side caller in preflight_linux.go is
// linux-only.
func parseMeminfo(s string) (free, total uint64, err error) {
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// "MemAvailable:    12345 kB"
		key := strings.TrimRight(fields[0], ":")
		val, perr := strconv.ParseUint(fields[1], 10, 64)
		if perr != nil {
			continue
		}
		switch key {
		case "MemAvailable":
			free = val / 1024 // kB -> MB
		case "MemTotal":
			total = val / 1024
		}
	}
	if total == 0 {
		return 0, 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
	}
	return free, total, nil
}

// HostResources is a snapshot of the host's current free memory + free
// disk on a given path. ResourceProbe encapsulates how it's gathered so
// tests can swap in a stub.
type HostResources struct {
	FreeMemoryMB  uint64
	TotalMemoryMB uint64
	FreeDiskBytes uint64
	DiskPath      string // the path Statfs was called against, for the error
}

// ResourceProbe lets tests substitute synthetic readings for the
// platform-specific helpers (which would otherwise have to be mocked
// via package-level vars). Production code uses DefaultProbe.
type ResourceProbe interface {
	// FreeMemoryMB returns (free, total) memory in MB.
	FreeMemoryMB() (free, total uint64, err error)
	// FreeDiskBytes returns free bytes on the partition holding the given path.
	FreeDiskBytes(path string) (uint64, error)
}

// DefaultProbe uses the platform-specific helpers (preflight_darwin.go,
// preflight_linux.go, preflight_other.go).
type DefaultProbe struct{}

func (DefaultProbe) FreeMemoryMB() (uint64, uint64, error)    { return freeMemoryMB() }
func (DefaultProbe) FreeDiskBytes(path string) (uint64, error) { return freeDiskBytes(path) }

// PreflightOptions tunes the threshold rules. Zero values fall back to
// HostHeadroomMB / DiskHeadroomMultiplier below.
type PreflightOptions struct {
	HostHeadroomMB        uint64 // host memory to keep free beyond the VM's request
	DiskHeadroomMultiplier uint64 // free disk must be at least N * VM disk
}

const (
	// DefaultHostHeadroomMB is what we want to leave for the host OS +
	// other apps. 1 GB lets macOS/Linux keep the desktop usable while
	// the VM runs near its budget.
	DefaultHostHeadroomMB uint64 = 1024
	// DefaultDiskHeadroomMultiplier reflects vfkit's sparse-disk file:
	// it's allocated at MachineConfig.Disk GB but grows on use. 2x
	// gives room for cache layers + a buffer before the disk panics.
	DefaultDiskHeadroomMultiplier uint64 = 2
)

// CheckHostResources samples the host with the given probe and applies
// the preflight rules to the requested MachineConfig. Returns:
//   - nil when the host can comfortably provision the VM
//   - an error formatted for direct display to the user, with the
//     specific shortfall + a remediation hint
//
// `dataDir` is where vfkit will place the disk image; pass cfg.DataDir
// equivalent so the disk free check measures the right partition.
func CheckHostResources(probe ResourceProbe, cfg MachineConfig, dataDir string, opt PreflightOptions) error {
	if opt.HostHeadroomMB == 0 {
		opt.HostHeadroomMB = DefaultHostHeadroomMB
	}
	if opt.DiskHeadroomMultiplier == 0 {
		opt.DiskHeadroomMultiplier = DefaultDiskHeadroomMultiplier
	}

	freeMB, totalMB, err := probe.FreeMemoryMB()
	if err != nil {
		// Don't gate on a probe failure — the user might be on a
		// platform we haven't added support for yet. Log-and-skip is
		// safer than refusing to provision.
		return nil
	}
	freeDiskB, err := probe.FreeDiskBytes(dataDir)
	if err != nil {
		return nil
	}

	memMB := uint64(cfg.Memory)
	requiredMB := memMB + opt.HostHeadroomMB
	if freeMB < requiredMB {
		return &PreflightError{
			Kind:    PreflightMemory,
			Want:    requiredMB,
			Have:    freeMB,
			Total:   totalMB,
			Message: formatMemoryShortfall(memMB, opt.HostHeadroomMB, freeMB, totalMB),
		}
	}

	diskGB := uint64(cfg.Disk)
	requiredDisk := diskGB * (1024 * 1024 * 1024) * opt.DiskHeadroomMultiplier
	if freeDiskB < requiredDisk {
		return &PreflightError{
			Kind:    PreflightDisk,
			Want:    requiredDisk,
			Have:    freeDiskB,
			Message: formatDiskShortfall(diskGB, opt.DiskHeadroomMultiplier, freeDiskB, dataDir),
		}
	}

	return nil
}

// PreflightErrorKind tells callers which resource was short.
type PreflightErrorKind int

const (
	PreflightMemory PreflightErrorKind = iota
	PreflightDisk
)

// PreflightError is the typed error CheckHostResources returns. Callers
// can switch on Kind to decide between auto-prune flows vs hard-refuse;
// the default Error() string is what the user sees on stderr.
type PreflightError struct {
	Kind    PreflightErrorKind
	Want    uint64
	Have    uint64
	Total   uint64
	Message string
}

func (p *PreflightError) Error() string { return p.Message }

func formatMemoryShortfall(memMB, headroomMB, freeMB, totalMB uint64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "insufficient host memory for VM provisioning\n")
	fmt.Fprintf(&b, "  requested VM memory: %d MB\n", memMB)
	fmt.Fprintf(&b, "  required total (VM + %d MB host headroom): %d MB\n", headroomMB, memMB+headroomMB)
	fmt.Fprintf(&b, "  host free memory: %d MB", freeMB)
	if totalMB > 0 {
		fmt.Fprintf(&b, " (of %d MB total)", totalMB)
	}
	fmt.Fprintf(&b, "\n\n")
	fmt.Fprintf(&b, "options:\n")
	fmt.Fprintf(&b, "  - close apps to free memory and retry\n")
	if memMB > 2048 {
		fmt.Fprintf(&b, "  - request a smaller VM: --memory %d (or less)\n", memMB/2)
	}
	fmt.Fprintf(&b, "  - prune unused podman state: `ycode podman cleanup`\n")
	return b.String()
}

func formatDiskShortfall(diskGB, multiplier, freeBytes uint64, dataDir string) string {
	var b strings.Builder
	freeGB := freeBytes / (1024 * 1024 * 1024)
	fmt.Fprintf(&b, "insufficient host disk for VM provisioning\n")
	fmt.Fprintf(&b, "  requested VM disk: %d GB (sparse)\n", diskGB)
	fmt.Fprintf(&b, "  required free disk (%dx for sparse growth + safety): %d GB\n", multiplier, diskGB*multiplier)
	fmt.Fprintf(&b, "  host free disk on %s: %d GB\n\n", dataDir, freeGB)
	fmt.Fprintf(&b, "options:\n")
	fmt.Fprintf(&b, "  - free disk on the partition holding %s\n", dataDir)
	if diskGB > 20 {
		fmt.Fprintf(&b, "  - request a smaller VM disk: --disk %d (or less)\n", diskGB/2)
	}
	fmt.Fprintf(&b, "  - prune unused podman state: `ycode podman cleanup`\n")
	return b.String()
}
