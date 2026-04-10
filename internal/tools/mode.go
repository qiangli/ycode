package tools

import (
	"context"
	"encoding/json"
)

// PlanModeController manages plan mode state.
type PlanModeController interface {
	EnterPlanMode() (string, error)
	ExitPlanMode() (string, error)
	InPlanMode() bool
}

// RegisterModeHandlers registers EnterPlanMode and ExitPlanMode tool handlers.
func RegisterModeHandlers(r *Registry, ctrl PlanModeController) {
	if ctrl == nil {
		return
	}

	if spec, ok := r.Get("EnterPlanMode"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			return ctrl.EnterPlanMode()
		}
	}

	if spec, ok := r.Get("ExitPlanMode"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			return ctrl.ExitPlanMode()
		}
	}
}
