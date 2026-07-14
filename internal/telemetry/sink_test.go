package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJSONLSink_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	sink, err := NewJSONLSink(path)
	if err != nil {
		t.Fatalf("NewJSONLSink: %v", err)
	}

	event := &Event{
		Type:      "turn.start",
		Timestamp: time.Now(),
		Data:      map[string]string{"prompt": "hello"},
	}

	if err := sink.Emit(event); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		t.Fatal("expected non-empty file")
	}

	var decoded Event
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		t.Fatalf("json unmarshal: %v\ncontent: %s", err, content)
	}
	if decoded.Type != "turn.start" {
		t.Errorf("expected type 'turn.start', got %q", decoded.Type)
	}
}

func TestJSONLSink_MultipleEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	sink, err := NewJSONLSink(path)
	if err != nil {
		t.Fatalf("NewJSONLSink: %v", err)
	}

	events := []*Event{
		{Type: "turn.start", Timestamp: time.Now(), Data: map[string]string{"prompt": "hello"}},
		{Type: "tool.call", Timestamp: time.Now(), Data: map[string]any{"name": "bash", "input": "ls"}},
		{Type: "turn.end", Timestamp: time.Now(), Data: map[string]string{"status": "ok", "text": "output"}},
	}

	for _, e := range events {
		if err := sink.Emit(e); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	var decoded Event
	for i, line := range lines {
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("line %d: json unmarshal: %v", i+1, err)
		}
		if decoded.Type != events[i].Type {
			t.Errorf("line %d: expected type %q, got %q", i+1, events[i].Type, decoded.Type)
		}
	}
}

func TestMemorySink_Events(t *testing.T) {
	sink := NewMemorySink()

	sink.Emit(&Event{Type: "turn.start", Timestamp: time.Now()})
	sink.Emit(&Event{Type: "turn.end", Timestamp: time.Now()})

	events := sink.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "turn.start" {
		t.Errorf("expected 'turn.start', got %q", events[0].Type)
	}
	if events[1].Type != "turn.end" {
		t.Errorf("expected 'turn.end', got %q", events[1].Type)
	}
}
