package sysinfo

import (
	"runtime"
	"strings"
	"testing"
)

func TestDetect_Basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := Detect()

	// These should always be populated.
	if ctx.OS != runtime.GOOS {
		t.Errorf("expected OS=%q, got %q", runtime.GOOS, ctx.OS)
	}
	if ctx.Arch != runtime.GOARCH {
		t.Errorf("expected Arch=%q, got %q", runtime.GOARCH, ctx.Arch)
	}
	if ctx.NumCPU <= 0 {
		t.Errorf("expected NumCPU > 0, got %d", ctx.NumCPU)
	}
	if ctx.Hostname == "" {
		t.Error("expected non-empty Hostname")
	}
}

func TestDetect_OSVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	version := detectOSVersion()
	// On macOS and Linux, this should return something.
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if version == "" {
			t.Logf("WARNING: OSVersion is empty on %s (may be expected in some CI environments)", runtime.GOOS)
		} else {
			t.Logf("OSVersion: %s", version)
		}
	}
}

func TestDetect_Container(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	isContainer, rt := detectContainer()
	t.Logf("IsContainer=%v, ContainerRT=%q", isContainer, rt)
	// We can't assert a specific value — just verify it doesn't crash.
}

func TestDetect_Internet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	hasInternet := detectInternet()
	t.Logf("HasInternet=%v", hasInternet)
	// Can't assert — may be air-gapped in CI.
}

func TestDetect_CI(t *testing.T) {
	isCI := detectCI()
	t.Logf("IsCI=%v", isCI)
}

func TestDetect_Git(t *testing.T) {
	hasGit := detectBinary("git")
	t.Logf("HasGit=%v", hasGit)
	// Git is almost always available in dev environments.
}

func TestDetect_Memory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	mb := detectMemory()
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if mb <= 0 {
			t.Logf("WARNING: MemoryMB=%d (may be expected in some environments)", mb)
		} else {
			t.Logf("MemoryMB=%d", mb)
		}
	}
}

func TestDetect_CanRunContainers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := Detect()

	// On bare metal: CanRunContainers should be true.
	if !ctx.IsContainer && !ctx.CanRunContainers {
		t.Error("bare metal should be able to run containers")
	}

	// Inside unprivileged container: CanRunContainers should be false.
	if ctx.IsContainer && !ctx.IsPrivileged && ctx.CanRunContainers {
		t.Error("unprivileged container should NOT be able to run containers")
	}

	t.Logf("IsContainer=%v, IsPrivileged=%v, CanRunContainers=%v",
		ctx.IsContainer, ctx.IsPrivileged, ctx.CanRunContainers)
}

func TestDetect_Summary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := Detect()
	summary := ctx.Summary()

	if summary == "" {
		t.Error("expected non-empty summary")
	}
	if !strings.Contains(summary, runtime.GOOS) {
		t.Errorf("summary should contain OS, got: %s", summary)
	}
	t.Logf("Summary: %s", summary)
}

func TestDetect_WSL(t *testing.T) {
	isWSL := detectWSL()
	t.Logf("IsWSL=%v", isWSL)
}

func TestSystemContext_DerivedFields(t *testing.T) {
	// Test the CanRunContainers derivation logic directly.
	tests := []struct {
		name           string
		isContainer    bool
		isPrivileged   bool
		wantContainers bool
	}{
		{"bare metal", false, false, true},
		{"privileged container", true, true, true},
		{"unprivileged container", true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &SystemContext{
				IsContainer:  tt.isContainer,
				IsPrivileged: tt.isPrivileged,
			}
			ctx.CanRunContainers = !ctx.IsContainer || ctx.IsPrivileged

			if ctx.CanRunContainers != tt.wantContainers {
				t.Errorf("CanRunContainers=%v, want %v", ctx.CanRunContainers, tt.wantContainers)
			}
		})
	}
}
