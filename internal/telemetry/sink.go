package telemetry

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Event is a telemetry event.
type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}

// Sink is the interface for telemetry consumers.
type Sink interface {
	Emit(event *Event) error
	Close() error
}

// JSONLSink writes events as JSONL to a file.
type JSONLSink struct {
	mu   sync.Mutex
	file *os.File
}

// NewJSONLSink creates a JSONL telemetry sink.
func NewJSONLSink(path string) (*JSONLSink, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &JSONLSink{file: f}, nil
}

// Emit writes an event to the file.
func (s *JSONLSink) Emit(event *Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = s.file.Write(data)
	return err
}

// Close closes the file.
func (s *JSONLSink) Close() error {
	return s.file.Close()
}

// MemorySink stores events in memory (for testing).
type MemorySink struct {
	mu     sync.Mutex
	events []*Event
}

// NewMemorySink creates an in-memory telemetry sink.
func NewMemorySink() *MemorySink {
	return &MemorySink{}
}

// Emit stores an event.
func (s *MemorySink) Emit(event *Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

// Close is a no-op for memory sink.
func (s *MemorySink) Close() error { return nil }

// Events returns all stored events.
func (s *MemorySink) Events() []*Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Event{}, s.events...)
}
