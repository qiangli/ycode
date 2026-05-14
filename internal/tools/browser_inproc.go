package tools

import (
	"context"

	"github.com/qiangli/ycode/internal/runtime/browser"
	"github.com/qiangli/ycode/internal/runtime/mcpservers"
	"github.com/qiangli/ycode/pkg/browser/wire"
)

// NewInprocClient returns a browser.Client that dispatches every
// wire.Action through the given in-process mcpservers.Manager. The
// live / probe / solo backends plus the reliability layer all live
// behind this seam.
//
// Wire it into the runtime by decorating rootCtx:
//
//	if cli := NewInprocClient(mgr); cli != nil {
//	    rootCtx = browser.WithClient(rootCtx, cli)
//	}
func NewInprocClient(mgr *mcpservers.Manager) browser.Client {
	if mgr == nil {
		return nil
	}
	return &inprocClient{mgr: mgr}
}

type inprocClient struct {
	mgr *mcpservers.Manager
}

func (c *inprocClient) Execute(ctx context.Context, action wire.Action) (*wire.Result, error) {
	// mcpservers.BrowserAction / BrowserResult are type aliases for
	// wire.Action / wire.Result (see internal/runtime/mcpservers/types.go),
	// so no copy is needed.
	return c.mgr.Execute(ctx, action)
}

func (c *inprocClient) Close() error {
	// Backend lifecycle (StopAll) is managed by the caller that
	// constructed the Manager; the Client itself owns no resources.
	return nil
}
