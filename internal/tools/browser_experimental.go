//go:build experimental

package tools

import (
	"context"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// SetBrowserManager wires an mcpservers.Manager into the browser_*
// tool dispatch path. Every browser_navigate/click/type/… call routes
// through the manager's selected mode (live, probe, or solo).
// Pass nil to clear.
func SetBrowserManager(mgr *mcpservers.Manager) {
	if mgr == nil {
		SetBrowserDispatchHook(nil)
		return
	}
	SetBrowserDispatchHook(func(ctx context.Context, action BrowserAction) (*BrowserResult, error) {
		mAction := mcpservers.BrowserAction{
			Type:      action.Type,
			URL:       action.URL,
			ElementID: action.ElementID,
			Selector:  action.Selector,
			Text:      action.Text,
			Direction: action.Direction,
			Amount:    action.Amount,
			Goal:      action.Goal,
			TabID:     action.TabID,
			TabAction: action.TabAction,
			Script:    action.Script,
			URLs:      action.URLs,
		}
		mr, err := mgr.Execute(ctx, mAction)
		if err != nil {
			return nil, err
		}
		return &BrowserResult{
			Success:      mr.Success,
			Title:        mr.Title,
			URL:          mr.URL,
			Content:      mr.Content,
			Elements:     mr.Elements,
			Data:         mr.Data,
			Image:        mr.Image,
			Error:        mr.Error,
			Hints:        mr.Hints,
			OutcomeClass: mr.OutcomeClass,
		}, nil
	})
}
