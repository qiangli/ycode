package ycode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/qiangli/coreutils/pkg/lockfile"
	"github.com/qiangli/ycode/internal/harness/event"
)

func (h *Harness) checkControlOwner(sessionID string) error {
	lock, err := h.lockSession(sessionID)
	if errors.Is(err, lockfile.ErrHeld) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("control: check owning session lock: %w", err)
	}
	if err := lock.Release(); err != nil {
		return fmt.Errorf("control: release owning session lock probe: %w", err)
	}
	return errors.New("control: owner lost before acknowledgment; completion uncertain")
}

// controlLiveRun uses the trusted, locked event store as a durable mailbox.
// Requests bind both the configuration and exact run; only its owning process
// acknowledges them. An acknowledgment is evidence, not a guessed PID signal.
func (h *Harness) controlLiveRun(ctx context.Context, sessionID, action string) error {
	if err := h.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := h.Session(sessionID)
	if err != nil {
		return err
	}
	sessionID = s.ID
	items, err := event.Replay(h.eventPath)
	if err != nil {
		return err
	}
	runID := ""
	for _, item := range items {
		if item.SessionID != sessionID {
			continue
		}
		if item.Type == "session.requested" {
			if item.ConfigDigest != h.doc.ConfigDigest {
				return errors.New("control configuration mismatch")
			}
			runID = item.RunID
		}
		if item.RunID == runID && item.Type == "session.run-finished" {
			runID = ""
		}
	}
	if runID == "" {
		return errors.New("control: no live turn")
	}
	// An unlocked session cannot have a live owning turn (including after a
	// crash). Never leave a stale command for a future run to pick up.
	if err := h.checkControlOwner(sessionID); err != nil {
		return err
	}
	request, err := h.events.Append(event.Draft{SessionID: sessionID, RunID: runID, StageID: "session.control", Type: "session.control-requested", ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"action": action}})
	if err != nil {
		return err
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		// Probe before replay so an acknowledgment written just before the
		// owner released its lock still wins over the owner-lost result.
		ownerErr := h.checkControlOwner(sessionID)
		items, err := event.Replay(h.eventPath)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.Sequence <= request.Sequence || item.SessionID != sessionID || item.RunID != runID {
				continue
			}
			if item.CausationID == request.Digest && item.Type == "session.control-acknowledged" {
				var data struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(item.Data, &data); err != nil {
					return err
				}
				if data.Error != "" {
					return errors.New(data.Error)
				}
				return nil
			}
			if item.Type == "session.run-finished" {
				return errors.New("control: turn finished before acknowledgment")
			}
		}
		if ownerErr != nil {
			return ownerErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// monitorControls is owned and joined by the turn goroutine. It can request a
// pause during a long stage, but acknowledges only when all stages have left.
func (h *Harness) monitorControls(ctx context.Context, sessionID, runID string, after uint64, active *activeRun) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var pending []Event
	var reached <-chan struct{}
	ack := func(request Event, failure string) bool {
		_, err := h.events.Append(event.Draft{SessionID: sessionID, RunID: runID, StageID: "session.control", Type: "session.control-acknowledged", ConfigDigest: h.doc.ConfigDigest, CausationID: request.Digest, Data: map[string]any{"error": failure}})
		return err == nil
	}
	for {
		items, err := event.Replay(h.eventPath)
		if err != nil {
			active.cancel()
			return
		}
		for _, item := range items {
			if item.Sequence <= after {
				continue
			}
			after = item.Sequence
			if item.SessionID != sessionID || item.RunID != runID || item.Type != "session.control-requested" {
				continue
			}
			if item.ConfigDigest != h.doc.ConfigDigest {
				ack(item, "control configuration mismatch")
				continue
			}
			var data struct {
				Action string `json:"action"`
			}
			if json.Unmarshal(item.Data, &data) != nil {
				ack(item, "invalid control request")
				continue
			}
			switch data.Action {
			case "pause":
				reached = active.pause.request()
				if reached == nil {
					ack(item, "pause: turn finished before a safe boundary")
					continue
				}
				pending = append(pending, item)
			case "continue":
				for _, request := range pending {
					ack(request, "pause superseded by continue")
				}
				pending, reached = nil, nil
				if !active.pause.release() {
					ack(item, "continue: no paused live turn")
					continue
				}
				_, err := h.events.Append(event.Draft{SessionID: sessionID, RunID: runID, StageID: "session.continue", Type: "session.continued", ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"approval_consumed": false}})
				if err != nil || !ack(item, "") {
					active.cancel()
					return
				}
			default:
				ack(item, fmt.Sprintf("unknown control action %q", data.Action))
			}
		}
		select {
		case <-reached:
			_, err := h.events.Append(event.Draft{SessionID: sessionID, RunID: runID, StageID: "session.pause", Type: "session.paused", ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"tools_undone": false}})
			if err != nil {
				active.cancel()
				return
			}
			for _, request := range pending {
				if !ack(request, "") {
					active.cancel()
					return
				}
			}
			pending, reached = nil, nil
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
