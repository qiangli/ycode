package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/runtime/config"
)

// PlanModeState tracks what was saved when entering plan mode,
// so ExitPlanMode can restore it.
type PlanModeState struct {
	HadLocalOverride  bool   `json:"hadLocalOverride"`
	PreviousLocalMode string `json:"previousLocalMode"`
}

// PlanModeManager implements PlanModeController using file-based state.
type PlanModeManager struct {
	localConfigPath string // .ycode/settings.local.json
	stateFilePath   string // .ycode/tool-state/plan-mode.json
}

// NewPlanModeManager creates a new PlanModeManager for the given .ycode directory.
func NewPlanModeManager(ycodeDir string) *PlanModeManager {
	return &PlanModeManager{
		localConfigPath: filepath.Join(ycodeDir, "settings.local.json"),
		stateFilePath:   filepath.Join(ycodeDir, "tool-state", "plan-mode.json"),
	}
}

// EnterPlanMode enables plan mode by writing to settings.local.json
// and saving the previous state for later restoration.
func (m *PlanModeManager) EnterPlanMode() (string, error) {
	currentMode, hadMode := m.currentPermissionMode()
	state, err := m.readState()
	if err != nil {
		return "", fmt.Errorf("read plan mode state: %w", err)
	}

	// Already in plan mode.
	if currentMode == "plan" {
		if state != nil {
			// Managed by us — idempotent success.
			return "Already in plan mode.", nil
		}
		// Set externally — don't interfere but note it's unmanaged.
		return "Plan mode is active (set externally).", nil
	}

	// Save current state for restoration.
	newState := &PlanModeState{
		HadLocalOverride:  hadMode,
		PreviousLocalMode: currentMode,
	}
	if err := m.writeState(newState); err != nil {
		return "", fmt.Errorf("save plan mode state: %w", err)
	}

	// Set plan mode in settings.local.json.
	if err := config.SetLocalConfigField(m.localConfigPath, "permissionMode", "plan"); err != nil {
		_ = m.deleteState() // clean up on failure
		return "", fmt.Errorf("set plan mode: %w", err)
	}

	return "Entered plan mode. Write-modifying tools are now disabled.", nil
}

// ExitPlanMode restores the previous permission mode and cleans up state.
func (m *PlanModeManager) ExitPlanMode() (string, error) {
	state, err := m.readState()
	if err != nil {
		return "", fmt.Errorf("read plan mode state: %w", err)
	}
	if state == nil {
		return "Not in managed plan mode, nothing to restore.", nil
	}

	// Check if plan mode is still active (may have been changed externally).
	currentMode, _ := m.currentPermissionMode()
	if currentMode != "plan" {
		_ = m.deleteState()
		return "Mode was already changed from plan, cleared stale state.", nil
	}

	// Restore previous state.
	if state.HadLocalOverride {
		if err := config.SetLocalConfigField(m.localConfigPath, "permissionMode", state.PreviousLocalMode); err != nil {
			return "", fmt.Errorf("restore permission mode: %w", err)
		}
	} else {
		// Remove the field entirely — there was no prior override.
		if err := config.SetLocalConfigField(m.localConfigPath, "permissionMode", nil); err != nil {
			return "", fmt.Errorf("remove permission mode: %w", err)
		}
	}

	_ = m.deleteState()

	if state.HadLocalOverride && state.PreviousLocalMode != "" {
		return fmt.Sprintf("Exited plan mode. Restored permission mode to %q.", state.PreviousLocalMode), nil
	}
	return "Exited plan mode. All tools are now available.", nil
}

// InPlanMode returns whether plan mode is currently active.
func (m *PlanModeManager) InPlanMode() bool {
	mode, ok := m.currentPermissionMode()
	return ok && mode == "plan"
}

func (m *PlanModeManager) currentPermissionMode() (string, bool) {
	val, ok := config.GetLocalConfigField(m.localConfigPath, "permissionMode")
	if !ok {
		return "", false
	}
	s, ok := val.(string)
	return s, ok
}

func (m *PlanModeManager) readState() (*PlanModeState, error) {
	data, err := os.ReadFile(m.stateFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var state PlanModeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *PlanModeManager) writeState(state *PlanModeState) error {
	if err := os.MkdirAll(filepath.Dir(m.stateFilePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.stateFilePath, data, 0o644)
}

func (m *PlanModeManager) deleteState() error {
	err := os.Remove(m.stateFilePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
