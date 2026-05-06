package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/qiangli/ycode/internal/bus"
)

// PermissionRequester routes a tool's permission check to a remote client
// (web UI, VS Code extension, etc.) and waits for the response.
//
// An App's tool registry calls this via InstallRemotePermissionPrompter
// instead of an in-process TUI prompter. The mode is the required
// permission level as a string (e.g. "WorkspaceWrite") and input is the
// raw tool input JSON, which the requester may parse to enrich the
// outbound event payload (for example, computing a before/after diff for
// edit-style tools).
type PermissionRequester func(ctx context.Context, sessionID string, toolName string, mode string, input json.RawMessage) (bool, error)

// RequestPermission publishes a permission.request event for the given
// session and blocks until the corresponding RespondPermission call
// arrives or the context is cancelled.
//
// The event payload always carries {request_id, tool, mode}. For tools
// recognised as file mutations (write_file, edit_file, ...), the payload
// additionally carries an "edit" field with {file_path, before_text,
// after_text} so a client can render a diff preview.
func (s *LocalService) RequestPermission(ctx context.Context, sessionID, toolName, mode string, input json.RawMessage) (bool, error) {
	requestID := uuid.NewString()

	ch := make(chan bool, 1)
	s.permMu.Lock()
	s.permChans[requestID] = ch
	s.permMu.Unlock()
	defer func() {
		s.permMu.Lock()
		delete(s.permChans, requestID)
		s.permMu.Unlock()
	}()

	payload := map[string]any{
		"request_id": requestID,
		"tool":       toolName,
		"mode":       mode,
	}
	if edit := extractEditDetail(toolName, input, s.app.WorkDir()); edit != nil {
		payload["edit"] = edit
	}

	s.b.Publish(bus.Event{
		Type:      bus.EventPermissionReq,
		SessionID: sessionID,
		Data:      mustJSON(payload),
	})

	select {
	case allowed := <-ch:
		return allowed, nil
	case <-ctx.Done():
		return false, fmt.Errorf("permission request cancelled: %w", ctx.Err())
	}
}

// RequestPermission routes the call to the LocalService for the given
// session. Used by the server-side prompter wired in buildAPIStack.
func (m *MultiService) RequestPermission(ctx context.Context, sessionID, toolName, mode string, input json.RawMessage) (bool, error) {
	svc, err := m.localService(sessionID)
	if err != nil {
		return false, err
	}
	return svc.RequestPermission(ctx, sessionID, toolName, mode, input)
}
