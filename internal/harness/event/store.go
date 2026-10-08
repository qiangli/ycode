// Package event provides the canonical append-only record of a harness run.
package event

import (
	"bufio"
	"bytes"
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

	"github.com/qiangli/coreutils/pkg/lockfile"
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
	watchers       map[chan struct{}]struct{}
}

// Cursor identifies a verified position in an event log. It is suitable for
// constructing a TailReader after an append made through the same Store.
type Cursor struct {
	Offset   int64
	Sequence uint64
	Digest   string
}

// TailReader verifies and returns only events appended after its cursor. It
// never rereads the verified prefix of an append-only log.
type TailReader struct {
	mu       sync.Mutex
	path     string
	offset   int64
	next     uint64
	previous string
	bytes    int64
}

// Reader retains a verified event stream and extends it from an incremental
// tail. It is for consumers (such as session stages) that need the complete
// history without validating it again at every stage boundary.
type Reader struct {
	mu     sync.Mutex
	path   string
	loaded bool
	events []Event
	tail   *TailReader
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
	// The log is shared by every process that runs a harness on this host
	// (concurrent genie runs, a TUI and a one-off): hold the log's lock for
	// the append, and take the sequence and chain tip from the file itself,
	// because another process may have appended since this one last did.
	lock, err := lockfile.AcquireWithin(s.path+".lock", 30*time.Second,
		lockfile.Holder{Name: "ycode event log", PID: os.Getpid(), Intent: "append", Since: s.now().UTC()})
	if err != nil {
		return Event{}, fmt.Errorf("lock event log: %w", err)
	}
	defer lock.Release()
	if seq, digest, ok, err := readTail(s.path); err != nil {
		return Event{}, fmt.Errorf("read event log tail: %w", err)
	} else if ok {
		s.next, s.previousDigest = seq+1, digest
	}
	e.Sequence, e.PreviousDigest = s.next, s.previousDigest
	e.Digest, err = eventDigest(e)
	if err != nil {
		return Event{}, fmt.Errorf("digest event: %w", err)
	}
	line, err = json.Marshal(e)
	if err != nil {
		return Event{}, fmt.Errorf("marshal event: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return Event{}, fmt.Errorf("open event log: %w", err)
	}
	_, writeErr := f.Write(append(line, '\n'))
	if writeErr == nil {
		err = f.Sync()
	} else {
		err = writeErr
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
	s.notifyLocked()
	return e, nil
}

// Cursor returns the current verified tail. Callers that only need events
// written after this point can avoid replaying the shared history.
func (s *Store) Cursor() (Cursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := os.Stat(s.path)
	if err != nil {
		return Cursor{}, err
	}
	return Cursor{Offset: info.Size(), Sequence: s.next - 1, Digest: s.previousDigest}, nil
}

// NewTailReader starts at cursor. The cursor must come from a verified Store
// position; subsequent reads validate sequence and digest continuity.
func NewTailReader(path string, cursor Cursor) *TailReader {
	return &TailReader{path: path, offset: cursor.Offset, next: cursor.Sequence + 1, previous: cursor.Digest}
}

// NewReader creates a lazy incremental reader. Its first Replay validates the
// whole stream; later calls decode only newly appended records.
func NewReader(path string) *Reader { return &Reader{path: path} }

// Replay returns the complete verified stream, incrementally extending the
// cached prefix after the first call.
func (r *Reader) Replay() ([]Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.loaded {
		events, err := Replay(r.path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(r.path)
		if err != nil {
			return nil, err
		}
		cursor := Cursor{Offset: info.Size()}
		if len(events) > 0 {
			cursor.Sequence = events[len(events)-1].Sequence
			cursor.Digest = events[len(events)-1].Digest
		}
		r.events, r.tail, r.loaded = events, NewTailReader(r.path, cursor), true
	} else {
		appended, err := r.tail.Read()
		if err != nil {
			return nil, err
		}
		r.events = append(r.events, appended...)
	}
	return append([]Event(nil), r.events...), nil
}

// Read returns the newly appended, fully verified event tail.
func (r *TailReader) Read() ([]Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := os.Open(r.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < r.offset {
		return nil, errors.New("event log shrank after verified cursor")
	}
	if info.Size() == r.offset {
		return nil, nil
	}
	if _, err := f.Seek(r.offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	// Another process can be between its write and fsync while we poll. Leave
	// an incomplete final record for the next read instead of treating it as a
	// corrupt verified tail.
	complete := data
	if end := bytes.LastIndexByte(data, '\n'); end >= 0 {
		complete = data[:end+1]
	} else {
		return nil, nil
	}
	decoded, err := decodeTail(complete, r.next, r.previous)
	if err != nil {
		return nil, err
	}
	r.offset += int64(len(complete))
	r.bytes += int64(len(complete))
	if len(decoded) > 0 {
		r.next = decoded[len(decoded)-1].Sequence + 1
		r.previous = decoded[len(decoded)-1].Digest
	}
	return decoded, nil
}

// BytesRead reports bytes decoded from appended tails. It is primarily useful
// to verify that polling callers are not rereading an unchanged history.
func (r *TailReader) BytesRead() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bytes
}

// Watch returns a channel that receives a coalesced notification after each
// successful append through this store. The cancel function unregisters the
// watcher; callers must invoke it when they stop waiting.
func (s *Store) Watch() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	if s.watchers == nil {
		s.watchers = make(map[chan struct{}]struct{})
	}
	s.watchers[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.watchers, ch)
		close(ch)
		s.mu.Unlock()
	}
}

func (s *Store) notifyLocked() {
	for ch := range s.watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// readTail returns the sequence and digest of the log's last event, reading
// backwards from the end so an append does not replay the whole log.
func readTail(path string) (seq uint64, digest string, ok bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, "", false, err
	}
	size := info.Size()
	for window := int64(64 << 10); ; window *= 2 {
		if window > size {
			window = size
		}
		buf := make([]byte, window)
		if _, err := f.ReadAt(buf, size-window); err != nil && !errors.Is(err, io.EOF) {
			return 0, "", false, err
		}
		trimmed := strings.TrimRight(string(buf), "\n")
		if trimmed == "" {
			return 0, "", false, nil
		}
		i := strings.LastIndexByte(trimmed, '\n')
		if i < 0 && window < size {
			continue // the last line is longer than the window
		}
		var last struct {
			Sequence uint64 `json:"sequence"`
			Digest   string `json:"digest"`
		}
		if err := json.Unmarshal([]byte(trimmed[i+1:]), &last); err != nil {
			return 0, "", false, fmt.Errorf("decode last event: %w", err)
		}
		return last.Sequence, last.Digest, true, nil
	}
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

// decodeTail applies the same validation as Decode, starting immediately
// after a previously verified sequence and digest.
func decodeTail(data []byte, next uint64, previousDigest string) ([]Event, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var events []Event
	for line := 1; scanner.Scan(); line++ {
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("decode event line %d: %w", line, err)
		}
		if e.SchemaVersion != SchemaVersion {
			return nil, fmt.Errorf("event line %d: unsupported schema version %d", line, e.SchemaVersion)
		}
		expected := next + uint64(len(events))
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
		events = append(events, e)
		previousDigest = e.Digest
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read event log: %w", err)
	}
	return events, nil
}
