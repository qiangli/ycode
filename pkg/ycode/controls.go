package ycode

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/qiangli/coreutils/pkg/lockfile"
	"github.com/qiangli/ycode/internal/harness/event"
)

func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }

// The kernel lock also excludes turns/maintenance in another frontend process.
func (h *Harness) lockSession(id string) (*lockfile.Lock, error) {
	if id == "" {
		return nil, errors.New("session id is required")
	}
	name := fmt.Sprintf("%x.lock", sha256.Sum256([]byte(id)))
	return lockfile.TryAcquire(filepath.Join(h.control, "sessions", "locks", name), lockfile.Holder{Intent: "session turn or maintenance"})
}

// busySession requires h.mu. Controls serialize with turn admission, so a
// history/model update cannot race an already running turn's commit.
func (h *Harness) busySession(id string) bool {
	for key, run := range h.active {
		if sessionOf(key) == id && !run.aside {
			return true
		}
	}
	return false
}

// Pause waits for the next graph boundary, leaving the continuation live.
// It never interrupts a tool execution or consumes a HITL decision.
func (h *Harness) Pause(ctx context.Context, sessionID string) error {
	return h.controlLiveRun(ctx, sessionID, "pause")
}

// Btw answers a side query without committing it to conversation history.
func (h *Harness) Btw(ctx context.Context, request RunRequest) (<-chan Event, error) {
	request.Aside = true
	return h.Run(ctx, request)
}

func (h *Harness) Plan(ctx context.Context, request RunRequest) (<-chan Event, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mode, err := h.SessionMode(request.SessionID)
	if err != nil {
		return nil, err
	}
	if len(request.Body) > 0 || mode != "plan" {
		mode = "plan"
	} else {
		mode = "act"
	}
	if len(request.Body) == 0 {
		item, err := h.setSessionMode(request.SessionID, mode)
		if err != nil {
			return nil, err
		}
		out := make(chan Event, 1)
		out <- item
		close(out)
		return out, nil
	}
	request.Plan = true
	return h.run(ctx, request, func() error {
		_, err := h.events.Append(event.Draft{SessionID: request.SessionID, RunID: request.RunID, StageID: "session.mode", Type: "session.mode-selected", ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"mode": mode}})
		return err
	})
}

func (h *Harness) SessionMode(sessionID string) (string, error) {
	if err := h.Validate(); err != nil {
		return "", err
	}
	events, err := event.Replay(h.eventPath)
	if errors.Is(err, os.ErrNotExist) {
		return "act", nil
	}
	if err != nil {
		return "", err
	}
	for i := len(events) - 1; i >= 0; i-- {
		item := events[i]
		if item.SessionID == sessionID && item.Type == "session.mode-selected" {
			if item.ConfigDigest != h.doc.ConfigDigest {
				return "", errors.New("session mode configuration mismatch")
			}
			var data struct {
				Mode string `json:"mode"`
			}
			if err := json.Unmarshal(item.Data, &data); err != nil {
				return "", err
			}
			return data.Mode, nil
		}
	}
	return "act", nil
}
func (h *Harness) setSessionMode(sessionID, mode string) (Event, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	lock, err := h.lockSession(sessionID)
	if err != nil {
		return Event{}, err
	}
	defer lock.Release()
	agent := h.doc.Spec.Agents[h.doc.Spec.Runtime.DefaultAgentRef]
	if agent.SessionControls.PlanPrompt == "" {
		return Event{}, errors.New("planning is not declared")
	}
	return h.events.Append(event.Draft{SessionID: sessionID, RunID: uuid.NewString(), StageID: "session.mode", Type: "session.mode-selected", ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"mode": mode}})
}

// Retry reuses the last durable request, with fresh run/admission identities.
// This is another execution attempt; prior tool effects are not undone.
func (h *Harness) Retry(ctx context.Context, request RunRequest) (<-chan Event, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	if request.Principal == "" || request.FrontendRef == "" || request.TriggerRef == "" {
		return nil, errors.New("retry requires the current caller principal, frontend and trigger")
	}
	events, err := event.Replay(h.eventPath)
	if err != nil {
		return nil, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		item := events[i]
		if item.SessionID != request.SessionID || item.Type != "session.requested" {
			continue
		}
		if item.ConfigDigest != h.doc.ConfigDigest {
			return nil, errors.New("retry configuration differs from recorded request")
		}
		var data struct {
			Ref string `json:"request_ref"`
		}
		if err := json.Unmarshal(item.Data, &data); err != nil {
			return nil, err
		}
		raw, err := h.payloads.Get(data.Ref)
		if err != nil {
			return nil, err
		}
		var previous RunRequest
		if err := json.Unmarshal(raw, &previous); err != nil {
			return nil, err
		}
		previous.RunID = request.RunID
		if previous.RunID == "" {
			previous.RunID = uuid.NewString()
		}
		previous.IdempotencyKey = previous.RunID
		if len(request.Body) > 0 {
			previous.Body = request.Body
		}
		previous.Principal, previous.FrontendRef, previous.TriggerRef = request.Principal, request.FrontendRef, request.TriggerRef
		previous.AgentRef = h.doc.Spec.Triggers[request.TriggerRef].Route.AgentRef
		// Human availability is a property of the current caller, not old UI state.
		previous.HumanAvailable = request.HumanAvailable
		targetRun := item.RunID
		return h.run(ctx, previous, func() error {
			current, err := event.Replay(h.eventPath)
			if err != nil {
				return err
			}
			last := ""
			committed, reverted := false, false
			for _, item := range current {
				if item.SessionID != request.SessionID {
					continue
				}
				if item.Type == "session.requested" {
					last = item.RunID
				}
				if item.RunID == targetRun && item.Type == "session.turn-committed" {
					committed = true
				}
				if committed && item.Type == "session.history-replaced" {
					var data struct {
						Reason string `json:"reason"`
					}
					if err := json.Unmarshal(item.Data, &data); err != nil {
						return err
					}
					if data.Reason == "revert" {
						reverted = true
					}
				}
			}
			if last != targetRun {
				return errors.New("retry: session advanced while preparing request")
			}
			if committed && !reverted {
				ref, err := h.revertHistoryRef(request.SessionID)
				if err != nil {
					return err
				}
				return h.replaceHistory(request.SessionID, uuid.NewString(), ref, map[string]any{"reason": "revert", "files_restored": false, "retry_of": targetRun})
			}
			return nil
		})
	}
	return nil, errors.New("retry: no recorded request")
}

func (h *Harness) validateSessionModel(agentRef, modelRef string) error {
	agent, ok := h.doc.Spec.Agents[agentRef]
	if !ok {
		return fmt.Errorf("undeclared agent %q", agentRef)
	}
	if _, ok := h.doc.Spec.Models[modelRef]; !ok {
		return fmt.Errorf("undeclared model %q", modelRef)
	}
	for _, attempt := range h.doc.Spec.Routes[agent.ModelRouteRef].Attempts {
		if attempt.ModelRef == modelRef {
			return nil
		}
	}
	return fmt.Errorf("model %q is not in agent %q's declared route", modelRef, agentRef)
}

func (h *Harness) sessionModel(sessionID string) (string, error) {
	events, err := event.Replay(h.eventPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for i := len(events) - 1; i >= 0; i-- {
		item := events[i]
		if item.SessionID != sessionID {
			continue
		}
		if item.Type == "session.model-selected" || item.Type == "session.forked" {
			if item.ConfigDigest != h.doc.ConfigDigest {
				return "", errors.New("session model selection belongs to a different configuration")
			}
			var data struct {
				ModelRef string `json:"model_ref"`
			}
			err := json.Unmarshal(item.Data, &data)
			return data.ModelRef, err
		}
	}
	return "", nil
}

// SessionModelOverride returns the session's explicit model selection
// resource key, or "" when the session follows its route default.
func (h *Harness) SessionModelOverride(sessionID string) (string, error) {
	if err := h.Validate(); err != nil {
		return "", err
	}
	return h.sessionModel(sessionID)
}

// SessionModel returns the effective model resource key, not provider ID.
func (h *Harness) SessionModel(sessionID string) (string, error) {
	if err := h.Validate(); err != nil {
		return "", err
	}
	ref, err := h.sessionModel(sessionID)
	if err != nil {
		return "", err
	}
	agent := h.doc.Spec.Runtime.DefaultAgentRef
	if ref != "" {
		return ref, h.validateSessionModel(agent, ref)
	}
	route := h.doc.Spec.Routes[h.doc.Spec.Agents[agent].ModelRouteRef]
	if len(route.Attempts) == 0 {
		return "", errors.New("agent has no model route")
	}
	return route.Attempts[0].ModelRef, nil
}

// SetSessionModel changes only this session; future runs revalidate the
// selection against their agent's declared route. An empty ref resets it.
func (h *Harness) SetSessionModel(sessionID, modelRef string) error {
	if err := h.Validate(); err != nil {
		return err
	}
	if sessionID == "" {
		return errors.New("model selection requires a session")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	lock, err := h.lockSession(sessionID)
	if err != nil {
		return err
	}
	defer lock.Release()
	if h.busySession(sessionID) {
		return errors.New("pause the active turn before switching models")
	}
	if modelRef != "" {
		if err := h.validateSessionModel(h.doc.Spec.Runtime.DefaultAgentRef, modelRef); err != nil {
			return err
		}
	}
	_, err = h.events.Append(event.Draft{SessionID: sessionID, RunID: uuid.NewString(), StageID: "session.model", Type: "session.model-selected", ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"model_ref": modelRef}})
	return err
}

type CompactResult struct {
	Outcome     string `json:"outcome"`
	MessagesRef string `json:"messages_ref"`
}

func (h *Harness) Compact(ctx context.Context, sessionID string) (CompactResult, error) {
	if err := h.Validate(); err != nil {
		return CompactResult{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.Session(sessionID)
	if err != nil {
		return CompactResult{}, err
	}
	sessionID = s.ID
	lock, err := h.lockSession(sessionID)
	if err != nil {
		return CompactResult{}, err
	}
	defer lock.Release()
	if h.busySession(sessionID) {
		return CompactResult{}, errors.New("pause the active turn before compacting")
	}
	runID := uuid.NewString()
	result, err := h.turn.CompactSession(ctx, s.ID, runID, h.doc.Spec.Runtime.DefaultAgentRef)
	if err == nil {
		err = h.replaceHistory(s.ID, runID, result.MessagesRef, map[string]any{"reason": "compact", "outcome": result.Outcome})
	}
	return CompactResult{Outcome: result.Outcome, MessagesRef: result.MessagesRef}, err
}

type RevertResult struct {
	TranscriptReverted bool   `json:"transcript_reverted"`
	FilesRestored      bool   `json:"files_restored"`
	Restoration        string `json:"restoration"`
	MessagesRef        string `json:"messages_ref"`
}

// Revert rewinds the last user turn in conversation history. The current
// Bashy execution boundary has no undo API: filesystem restoration is always
// explicitly unsupported, never inferred from a transcript or checkpoint.
func (h *Harness) Revert(ctx context.Context, sessionID string) (RevertResult, error) {
	result := RevertResult{Restoration: "unsupported: execution boundary exposes no tool undo capability; files have not been restored"}
	if err := h.Validate(); err != nil {
		return result, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.Session(sessionID)
	if err != nil {
		return result, err
	}
	sessionID = s.ID
	lock, err := h.lockSession(sessionID)
	if err != nil {
		return result, err
	}
	defer lock.Release()
	if h.busySession(sessionID) {
		return result, errors.New("pause the active turn before reverting")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref, err := h.revertHistoryRef(s.ID)
	if err != nil {
		return result, err
	}
	result.MessagesRef = ref
	err = h.replaceHistory(s.ID, uuid.NewString(), ref, map[string]any{"reason": "revert", "files_restored": false, "restoration": result.Restoration})
	result.TranscriptReverted = err == nil
	return result, err
}

// A turn may contain several user-role steering messages. Rewind its actual
// loaded history boundary, not a guessed role boundary inside the transcript.
func (h *Harness) revertHistoryRef(sessionID string) (string, error) {
	events, err := event.Replay(h.eventPath)
	if err != nil {
		return "", err
	}
	var stack []string
	loaded := map[string]string{}
	current := ""
	for _, item := range events {
		if item.SessionID != sessionID {
			continue
		}
		var data struct {
			Ref    string `json:"messages_ref"`
			Parent string `json:"parent_messages_ref"`
			Reason string `json:"reason"`
		}
		switch item.Type {
		case "session.history.loaded", "session.turn-committed", "session.history-replaced", "session.forked":
			if err := json.Unmarshal(item.Data, &data); err != nil {
				return "", err
			}
		default:
			continue
		}
		switch item.Type {
		case "session.history.loaded":
			loaded[item.RunID] = data.Ref
		case "session.turn-committed":
			before, ok := loaded[item.RunID]
			if !ok {
				before = current
			}
			stack = append(stack, before)
			current = data.Ref
		case "session.history-replaced":
			if data.Reason == "revert" && len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			current = data.Ref
		case "session.forked":
			current = data.Parent
		}
	}
	if len(stack) == 0 {
		return "", errors.New("revert: no committed turn in this session")
	}
	ref := stack[len(stack)-1]
	if ref == "" {
		return h.payloads.Put([]byte("[]"))
	}
	if _, err := h.payloads.Get(ref); err != nil {
		return "", err
	}
	return ref, nil
}

func (h *Harness) replaceHistory(sessionID, runID, ref string, data map[string]any) error {
	data["messages_ref"] = ref
	model, err := h.sessionModel(sessionID)
	if err != nil {
		return err
	}
	_, err = h.events.Append(event.Draft{SessionID: sessionID, RunID: runID, StageID: "session.history", Type: "session.history-replaced", ConfigDigest: h.doc.ConfigDigest, Data: data})
	if err != nil {
		return err
	}
	_, err = h.events.SaveCheckpoint(h.boundaryPath(sessionID, runID), sessionID, runID, turnBoundary{ConfigDigest: h.doc.ConfigDigest, SessionID: sessionID, RunID: runID, MessagesRef: ref, ModelRef: model})
	return err
}
