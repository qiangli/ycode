package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/config"
)

func newTestManager(t *testing.T) *PlanModeManager {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".agents", "ycode")
	os.MkdirAll(dir, 0o755)
	return NewPlanModeManager(dir)
}

func TestEnterPlanMode_Fresh(t *testing.T) {
	mgr := newTestManager(t)

	msg, err := mgr.EnterPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	if msg == "" {
		t.Error("expected non-empty message")
	}

	// settings.local.json should have permissionMode: "plan"
	val, ok := config.GetLocalConfigField(mgr.localConfigPath, "permissionMode")
	if !ok || val != "plan" {
		t.Errorf("expected permissionMode=plan, got %v (ok=%v)", val, ok)
	}

	// State file should exist with hadLocalOverride=false
	state, err := mgr.readState()
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected state file to exist")
	}
	if state.HadLocalOverride {
		t.Error("expected hadLocalOverride=false")
	}

	if !mgr.InPlanMode() {
		t.Error("InPlanMode should return true")
	}
}

func TestEnterPlanMode_ExistingMode(t *testing.T) {
	mgr := newTestManager(t)

	// Set an existing permission mode.
	if err := config.SetLocalConfigField(mgr.localConfigPath, "permissionMode", "workspace-write"); err != nil {
		t.Fatal(err)
	}

	msg, err := mgr.EnterPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	if msg == "" {
		t.Error("expected non-empty message")
	}

	// State should record the previous mode.
	state, err := mgr.readState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.HadLocalOverride {
		t.Error("expected hadLocalOverride=true")
	}
	if state.PreviousLocalMode != "workspace-write" {
		t.Errorf("expected previousLocalMode=workspace-write, got %q", state.PreviousLocalMode)
	}
}

func TestEnterPlanMode_Idempotent(t *testing.T) {
	mgr := newTestManager(t)

	// Enter twice.
	if _, err := mgr.EnterPlanMode(); err != nil {
		t.Fatal(err)
	}
	msg, err := mgr.EnterPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	if msg != "Already in plan mode." {
		t.Errorf("expected idempotent message, got %q", msg)
	}
}

func TestExitPlanMode_NoState(t *testing.T) {
	mgr := newTestManager(t)

	msg, err := mgr.ExitPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	if msg != "Not in managed plan mode, nothing to restore." {
		t.Errorf("unexpected message: %q", msg)
	}
}

func TestExitPlanMode_RestorePrevious(t *testing.T) {
	mgr := newTestManager(t)

	// Set existing mode, enter plan, then exit.
	if err := config.SetLocalConfigField(mgr.localConfigPath, "permissionMode", "workspace-write"); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.EnterPlanMode(); err != nil {
		t.Fatal(err)
	}

	msg, err := mgr.ExitPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	if msg == "" {
		t.Error("expected non-empty message")
	}

	// Should be restored to workspace-write.
	val, ok := config.GetLocalConfigField(mgr.localConfigPath, "permissionMode")
	if !ok || val != "workspace-write" {
		t.Errorf("expected permissionMode=workspace-write, got %v", val)
	}

	// State file should be deleted.
	state, err := mgr.readState()
	if err != nil {
		t.Fatal(err)
	}
	if state != nil {
		t.Error("expected state file to be deleted")
	}

	if mgr.InPlanMode() {
		t.Error("InPlanMode should return false after exit")
	}
}

func TestExitPlanMode_RemoveOverride(t *testing.T) {
	mgr := newTestManager(t)

	// Enter from clean state (no prior override).
	if _, err := mgr.EnterPlanMode(); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.ExitPlanMode(); err != nil {
		t.Fatal(err)
	}

	// permissionMode should be gone.
	_, ok := config.GetLocalConfigField(mgr.localConfigPath, "permissionMode")
	if ok {
		t.Error("expected permissionMode to be removed from settings.local.json")
	}
}

func TestExitPlanMode_StaleState(t *testing.T) {
	mgr := newTestManager(t)

	// Enter plan mode.
	if _, err := mgr.EnterPlanMode(); err != nil {
		t.Fatal(err)
	}

	// Externally change the mode away from plan.
	if err := config.SetLocalConfigField(mgr.localConfigPath, "permissionMode", "danger-full-access"); err != nil {
		t.Fatal(err)
	}

	msg, err := mgr.ExitPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	if msg != "Mode was already changed from plan, cleared stale state." {
		t.Errorf("unexpected message: %q", msg)
	}

	// Mode should remain as externally set.
	val, _ := config.GetLocalConfigField(mgr.localConfigPath, "permissionMode")
	if val != "danger-full-access" {
		t.Errorf("expected mode to remain danger-full-access, got %v", val)
	}

	// State file should be cleaned up.
	state, _ := mgr.readState()
	if state != nil {
		t.Error("expected stale state file to be deleted")
	}
}

func TestRoundTrip(t *testing.T) {
	mgr := newTestManager(t)

	// Set other fields that should survive the round trip.
	if err := config.SetLocalConfigField(mgr.localConfigPath, "model", "test-model"); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.EnterPlanMode(); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ExitPlanMode(); err != nil {
		t.Fatal(err)
	}

	// Other fields should still be there.
	val, ok := config.GetLocalConfigField(mgr.localConfigPath, "model")
	if !ok || val != "test-model" {
		t.Errorf("expected model=test-model to survive, got %v", val)
	}

	// permissionMode should be gone.
	_, ok = config.GetLocalConfigField(mgr.localConfigPath, "permissionMode")
	if ok {
		t.Error("expected permissionMode to be removed after round trip")
	}
}
