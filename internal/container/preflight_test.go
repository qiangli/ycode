package container

import (
	"errors"
	"strings"
	"testing"
)

// stubProbe lets each test pin the memory + disk numbers CheckHostResources
// sees, without any actual sysctl/statfs work. Independent stubs for the two
// readings let us tickle each error path in isolation.
type stubProbe struct {
	freeMB, totalMB uint64
	memErr          error
	freeDisk        uint64
	diskErr         error
}

func (s *stubProbe) FreeMemoryMB() (uint64, uint64, error) {
	return s.freeMB, s.totalMB, s.memErr
}
func (s *stubProbe) FreeDiskBytes(string) (uint64, error) {
	return s.freeDisk, s.diskErr
}

const gb = uint64(1024 * 1024 * 1024)

// TestPreflightMemorySufficient: ample free memory + ample disk → no
// error. The "happy path".
func TestPreflightMemorySufficient(t *testing.T) {
	probe := &stubProbe{
		freeMB: 16_000, totalMB: 32_000,
		freeDisk: 200 * gb,
	}
	err := CheckHostResources(probe, MachineConfig{Memory: 8192, Disk: 50}, "/tmp", PreflightOptions{})
	if err != nil {
		t.Errorf("unexpected error on sufficient resources: %v", err)
	}
}

// TestPreflightMemoryShortfall: VM wants 8 GB on a host with 4 GB free.
// The original failure mode. Error must be a PreflightError of Kind
// PreflightMemory, and the message must name the VM size + the free
// amount (operator-facing).
func TestPreflightMemoryShortfall(t *testing.T) {
	probe := &stubProbe{
		freeMB: 4096, totalMB: 8192,
		freeDisk: 200 * gb,
	}
	err := CheckHostResources(probe, MachineConfig{Memory: 8192, Disk: 50}, "/tmp", PreflightOptions{})
	if err == nil {
		t.Fatal("expected error when VM memory exceeds free memory")
	}
	var pe *PreflightError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PreflightError, got %T", err)
	}
	if pe.Kind != PreflightMemory {
		t.Errorf("Kind = %v, want PreflightMemory", pe.Kind)
	}
	if !strings.Contains(err.Error(), "8192") || !strings.Contains(err.Error(), "4096") {
		t.Errorf("error message should mention VM size + free amount: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "ycode podman cleanup") {
		t.Errorf("error should hint at cleanup remediation: %q", err.Error())
	}
}

// TestPreflightMemoryHeadroom: VM exactly equals free memory — fails
// because we require a 1 GB host headroom. Pins the "VM + headroom"
// rule.
func TestPreflightMemoryHeadroom(t *testing.T) {
	probe := &stubProbe{
		freeMB: 8192, totalMB: 16_000,
		freeDisk: 200 * gb,
	}
	err := CheckHostResources(probe, MachineConfig{Memory: 8192, Disk: 50}, "/tmp", PreflightOptions{})
	if err == nil {
		t.Fatal("expected error when free == VM (no host headroom)")
	}
	var pe *PreflightError
	if !errors.As(err, &pe) || pe.Kind != PreflightMemory {
		t.Errorf("expected PreflightMemory error, got %T %v", err, err)
	}
}

// TestPreflightDiskShortfall: ample memory but free disk is < 2 * VM
// disk. Catches the "50 GB sparse image on a 30 GB partition" case.
func TestPreflightDiskShortfall(t *testing.T) {
	probe := &stubProbe{
		freeMB: 16_000, totalMB: 32_000,
		freeDisk: 30 * gb,
	}
	err := CheckHostResources(probe, MachineConfig{Memory: 4096, Disk: 50}, "/tmp", PreflightOptions{})
	if err == nil {
		t.Fatal("expected error when free disk < 2 * VM disk")
	}
	var pe *PreflightError
	if !errors.As(err, &pe) || pe.Kind != PreflightDisk {
		t.Errorf("expected PreflightDisk error, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "50 GB") || !strings.Contains(err.Error(), "30 GB") {
		t.Errorf("disk error should name VM + free: %q", err.Error())
	}
}

// TestPreflightCustomHeadroom: a stricter headroom catches cases the
// default would miss. Exposes the policy knob so test envs (CI on a
// 16 GB runner that's already mostly used) can tune the gate.
func TestPreflightCustomHeadroom(t *testing.T) {
	probe := &stubProbe{
		freeMB: 9000, totalMB: 16_000,
		freeDisk: 200 * gb,
	}
	// VM 8 GB + 512 MB headroom = 8704 MB; fits in 9000 → ok
	if err := CheckHostResources(probe, MachineConfig{Memory: 8192, Disk: 50}, "/tmp",
		PreflightOptions{HostHeadroomMB: 512}); err != nil {
		t.Errorf("smaller headroom should have allowed: %v", err)
	}
	// VM 8 GB + 4 GB headroom = 12288 MB; doesn't fit in 9000 → fail
	if err := CheckHostResources(probe, MachineConfig{Memory: 8192, Disk: 50}, "/tmp",
		PreflightOptions{HostHeadroomMB: 4096}); err == nil {
		t.Error("4 GB headroom should have failed at 9000 MB free")
	}
}

// TestPreflightProbeErrorIsSoftFail: when the probe itself errors (e.g.
// running on an unsupported platform), the preflight should NOT gate
// the operation. Returning nil lets the user attempt the provision
// rather than hard-refusing on a platform we haven't certified.
func TestPreflightProbeErrorIsSoftFail(t *testing.T) {
	probe := &stubProbe{memErr: errors.New("vm_stat: not found")}
	err := CheckHostResources(probe, MachineConfig{Memory: 8192, Disk: 50}, "/tmp", PreflightOptions{})
	if err != nil {
		t.Errorf("probe error should soft-fail; got %v", err)
	}
}

// TestPreflightDiskProbeErrorIsSoftFail: same for the disk probe.
func TestPreflightDiskProbeErrorIsSoftFail(t *testing.T) {
	probe := &stubProbe{
		freeMB: 16_000, totalMB: 32_000,
		diskErr: errors.New("statfs: not implemented"),
	}
	err := CheckHostResources(probe, MachineConfig{Memory: 8192, Disk: 50}, "/tmp", PreflightOptions{})
	if err != nil {
		t.Errorf("disk probe error should soft-fail; got %v", err)
	}
}

// TestParseVMStatFreeMB pins the macOS vm_stat parser against a sample
// of the actual output format. Covers: header page-size detection,
// per-pool counts, summing free+inactive+speculative+purgeable.
func TestParseVMStatFreeMB(t *testing.T) {
	sample := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                              100000.
Pages active:                            200000.
Pages inactive:                           50000.
Pages speculative:                        20000.
Pages throttled:                              0.
Pages wired down:                        300000.
Pages purgeable:                          10000.
"Translation faults":                  12345678.
`
	// Expected: (100000 + 50000 + 20000 + 10000) * 16384 bytes
	//         = 180000 * 16384 / (1024*1024) MB = 2812 MB
	got, err := parseVMStatFreeMB(sample)
	if err != nil {
		t.Fatalf("parseVMStatFreeMB: %v", err)
	}
	want := uint64(180000) * 16384 / (1024 * 1024)
	if got != want {
		t.Errorf("got %d MB, want %d MB", got, want)
	}
}

// TestParseMeminfo pins the Linux parser. Covers: MemAvailable +
// MemTotal extraction, kB→MB conversion.
func TestParseMeminfo(t *testing.T) {
	sample := `MemTotal:        8000000 kB
MemFree:         2000000 kB
MemAvailable:    6000000 kB
Buffers:          100000 kB
Cached:          3000000 kB
SwapTotal:             0 kB
`
	free, total, err := parseMeminfo(sample)
	if err != nil {
		t.Fatalf("parseMeminfo: %v", err)
	}
	if free != 6000000/1024 {
		t.Errorf("free = %d MB, want %d MB", free, 6000000/1024)
	}
	if total != 8000000/1024 {
		t.Errorf("total = %d MB, want %d MB", total, 8000000/1024)
	}
}
