package telemetry

import (
	"sync"
	"time"
)

// SessionTraceRecord represents a single trace entry in a session.
type SessionTraceRecord struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"` // api_call, tool_use, command, compaction, error
	Name        string         `json:"name,omitempty"`
	StartedAt   time.Time      `json:"started_at"`
	CompletedAt time.Time      `json:"completed_at,omitempty"`
	Duration    string         `json:"duration,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// SessionTracer records traces for a conversation session.
type SessionTracer struct {
	mu      sync.Mutex
	records []SessionTraceRecord
	sink    Sink
	nextID  int
}

// NewSessionTracer creates a session tracer with an optional sink.
func NewSessionTracer(sink Sink) *SessionTracer {
	return &SessionTracer{
		sink: sink,
	}
}

// Start begins a trace record and returns its ID.
func (st *SessionTracer) Start(traceType, name string) string {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.nextID++
	id := traceType + "_" + time.Now().Format("150405") + "_" + name

	record := SessionTraceRecord{
		ID:        id,
		Type:      traceType,
		Name:      name,
		StartedAt: time.Now(),
	}
	st.records = append(st.records, record)

	return id
}

// Complete marks a trace record as completed.
func (st *SessionTracer) Complete(id string, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	for i := range st.records {
		if st.records[i].ID == id {
			st.records[i].CompletedAt = time.Now()
			st.records[i].Duration = time.Since(st.records[i].StartedAt).String()
			if err != nil {
				st.records[i].Error = err.Error()
			}

			// Emit to sink.
			if st.sink != nil {
				_ = st.sink.Emit(&Event{
					Type:      "trace_complete",
					Timestamp: time.Now(),
					Data:      st.records[i],
				})
			}
			return
		}
	}
}

// AddMetadata attaches metadata to a trace record.
func (st *SessionTracer) AddMetadata(id string, key string, value any) {
	st.mu.Lock()
	defer st.mu.Unlock()

	for i := range st.records {
		if st.records[i].ID == id {
			if st.records[i].Metadata == nil {
				st.records[i].Metadata = make(map[string]any)
			}
			st.records[i].Metadata[key] = value
			return
		}
	}
}

// Records returns all trace records.
func (st *SessionTracer) Records() []SessionTraceRecord {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]SessionTraceRecord{}, st.records...)
}

// Summary returns a summary of the session trace.
func (st *SessionTracer) Summary() map[string]int {
	st.mu.Lock()
	defer st.mu.Unlock()

	counts := make(map[string]int)
	for _, r := range st.records {
		counts[r.Type]++
	}
	return counts
}
