// Package event provides the canonical append-only record of a harness run.
package event

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const SchemaVersion = 1

// Event is one durable state transition. Data is intentionally opaque to the
// store; stage implementations own their versioned payloads.
type Event struct {
	SchemaVersion  int             `json:"schema_version"`
	Sequence       uint64          `json:"sequence"`
	Time           time.Time       `json:"time"`
	SessionID      string          `json:"session_id"`
	RunID          string          `json:"run_id"`
	StageID        string          `json:"stage_id,omitempty"`
	Type           string          `json:"type"`
	CallID         string          `json:"call_id,omitempty"`
	CausationID    string          `json:"causation_id,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	ConfigDigest   string          `json:"config_digest,omitempty"`
	StateBefore    uint64          `json:"state_before,omitempty"`
	StateAfter     uint64          `json:"state_after,omitempty"`
	PayloadDigest  string          `json:"payload_digest,omitempty"`
	PreviousDigest string          `json:"previous_digest,omitempty"`
	Digest         string          `json:"digest"`
	Data           json.RawMessage `json:"data,omitempty"`
}

// Draft is an event before the store assigns durable ordering metadata.
type Draft struct {
	SessionID     string
	RunID         string
	StageID       string
	Type          string
	CallID        string
	CausationID   string
	CorrelationID string
	ConfigDigest  string
	StateBefore   uint64
	StateAfter    uint64
	Data          any
}

// Store appends newline-delimited JSON events and synchronizes each append.
type Store struct {
	mu             sync.Mutex
	path           string
	next           uint64
	previousDigest string
	now            func() time.Time
}

// Checkpoint is an atomic snapshot tied to an exact event-log position.
type Checkpoint struct {
	SchemaVersion int             `json:"schema_version"`
	Sequence      uint64          `json:"sequence"`
	SessionID     string          `json:"session_id"`
	RunID         string          `json:"run_id"`
	Time          time.Time       `json:"time"`
	State         json.RawMessage `json:"state"`
}

// Open validates the existing log and returns a store positioned at its tail.
func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("event log path: %w", err)
	}
	events, err := Replay(abs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	next := uint64(1)
	if len(events) > 0 {
		next = events[len(events)-1].Sequence + 1
	}
	previousDigest := ""
	if len(events) > 0 {
		previousDigest = events[len(events)-1].Digest
	}
	return &Store{path: abs, next: next, previousDigest: previousDigest, now: time.Now}, nil
}

// Append writes and fsyncs one event before returning it to the caller.
func (s *Store) Append(d Draft) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.SessionID == "" || d.RunID == "" || d.Type == "" {
		return Event{}, errors.New("event requires session_id, run_id and type")
	}
	data, err := redactedJSON(d.Data)
	if err != nil {
		return Event{}, fmt.Errorf("marshal event data: %w", err)
	}
	e := Event{SchemaVersion: SchemaVersion, Sequence: s.next, Time: s.now().UTC(), SessionID: d.SessionID, RunID: d.RunID, StageID: d.StageID, Type: d.Type, CallID: d.CallID, CausationID: d.CausationID, CorrelationID: d.CorrelationID, ConfigDigest: d.ConfigDigest, StateBefore: d.StateBefore, StateAfter: d.StateAfter, PayloadDigest: digestBytes(data), PreviousDigest: s.previousDigest, Data: data}
	e.Digest, err = eventDigest(e)
	if err != nil {
		return Event{}, fmt.Errorf("digest event: %w", err)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return Event{}, fmt.Errorf("marshal event: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return Event{}, fmt.Errorf("create event directory: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return Event{}, fmt.Errorf("open event log: %w", err)
	}
	if _, err = f.Write(append(line, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return Event{}, fmt.Errorf("append event: %w", err)
	}
	if closeErr != nil {
		return Event{}, fmt.Errorf("close event log: %w", closeErr)
	}
	s.next++
	s.previousDigest = e.Digest
	return e, nil
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func eventDigest(e Event) (string, error) {
	e.Digest = ""
	raw, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return digestBytes(raw), nil
}

// SaveCheckpoint atomically persists state after all events through the
// store's current sequence. It never advances the event stream.
func (s *Store) SaveCheckpoint(path, sessionID, runID string, state any) (Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sessionID == "" || runID == "" {
		return Checkpoint{}, errors.New("checkpoint requires session_id and run_id")
	}
	data, err := redactedJSON(state)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("marshal checkpoint state: %w", err)
	}
	cp := Checkpoint{SchemaVersion: SchemaVersion, Sequence: s.next - 1, SessionID: sessionID, RunID: runID, Time: s.now().UTC(), State: data}
	encoded, err := json.Marshal(cp)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("marshal checkpoint: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("checkpoint path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return Checkpoint{}, fmt.Errorf("create checkpoint directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".checkpoint-*")
	if err != nil {
		return Checkpoint{}, fmt.Errorf("create checkpoint: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(append(encoded, '\n'))
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Checkpoint{}, fmt.Errorf("write checkpoint: %w", err)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return Checkpoint{}, fmt.Errorf("publish checkpoint: %w", err)
	}
	return cp, nil
}

func redactedJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	redact(decoded)
	return json.Marshal(decoded)
}

func redact(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
			if strings.Contains(lower, "apikey") || strings.Contains(lower, "authorization") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") {
				typed[key] = "[REDACTED]"
				continue
			}
			redact(child)
		}
	case []any:
		for _, child := range typed {
			redact(child)
		}
	}
}

// Replay validates and returns the complete event stream.
func Replay(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Decode(f)
}

// Decode reads a JSONL stream and rejects unknown schema versions, gaps and
// Bashy results without a preceding request.
func Decode(r io.Reader) ([]Event, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var events []Event
	openCalls := make(map[string]struct{})
	previousDigest := ""
	for line := 1; scanner.Scan(); line++ {
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("decode event line %d: %w", line, err)
		}
		if e.SchemaVersion != SchemaVersion {
			return nil, fmt.Errorf("event line %d: unsupported schema version %d", line, e.SchemaVersion)
		}
		expected := uint64(len(events) + 1)
		if e.Sequence != expected {
			return nil, fmt.Errorf("event line %d: sequence %d, expected %d", line, e.Sequence, expected)
		}
		if e.SessionID == "" || e.RunID == "" || e.Type == "" || e.Time.IsZero() {
			return nil, fmt.Errorf("event line %d: missing required metadata", line)
		}
		if e.PreviousDigest != previousDigest {
			return nil, fmt.Errorf("event line %d: previous digest %q, expected %q", line, e.PreviousDigest, previousDigest)
		}
		if got := digestBytes(e.Data); e.PayloadDigest != got {
			return nil, fmt.Errorf("event line %d: payload digest mismatch", line)
		}
		wantDigest, err := eventDigest(e)
		if err != nil || e.Digest != wantDigest {
			return nil, fmt.Errorf("event line %d: event digest mismatch", line)
		}
		switch e.Type {
		case "bashy.requested":
			if e.CallID == "" {
				return nil, fmt.Errorf("event line %d: bashy request has no call_id", line)
			}
			if _, exists := openCalls[e.CallID]; exists {
				return nil, fmt.Errorf("event line %d: duplicate open call_id %q", line, e.CallID)
			}
			openCalls[e.CallID] = struct{}{}
		case "bashy.completed", "bashy.failed":
			if _, exists := openCalls[e.CallID]; !exists {
				return nil, fmt.Errorf("event line %d: Bashy result has no request for call_id %q", line, e.CallID)
			}
			delete(openCalls, e.CallID)
		}
		events = append(events, e)
		previousDigest = e.Digest
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read event log: %w", err)
	}
	return events, nil
}
