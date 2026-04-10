package api

import (
	"io"
	"strings"
	"testing"
)

func TestSSEParser_BasicEvent(t *testing.T) {
	input := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"
	parser := NewSSEParser(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Event != "message_start" {
		t.Errorf("expected event 'message_start', got %q", event.Event)
	}
	if event.Data != `{"type":"message_start"}` {
		t.Errorf("unexpected data: %q", event.Data)
	}
}

func TestSSEParser_MultipleEvents(t *testing.T) {
	input := "event: a\ndata: first\n\nevent: b\ndata: second\n\n"
	parser := NewSSEParser(strings.NewReader(input))

	ev1, err := parser.Next()
	if err != nil {
		t.Fatalf("event 1 error: %v", err)
	}
	if ev1.Event != "a" || ev1.Data != "first" {
		t.Errorf("event 1: got %q/%q", ev1.Event, ev1.Data)
	}

	ev2, err := parser.Next()
	if err != nil {
		t.Fatalf("event 2 error: %v", err)
	}
	if ev2.Event != "b" || ev2.Data != "second" {
		t.Errorf("event 2: got %q/%q", ev2.Event, ev2.Data)
	}

	_, err = parser.Next()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestSSEParser_MultilineData(t *testing.T) {
	input := "data: line1\ndata: line2\n\n"
	parser := NewSSEParser(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Data != "line1\nline2" {
		t.Errorf("unexpected data: %q", event.Data)
	}
}

func TestSSEParser_CommentSkipped(t *testing.T) {
	input := ": this is a comment\nevent: test\ndata: hello\n\n"
	parser := NewSSEParser(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Event != "test" || event.Data != "hello" {
		t.Errorf("unexpected: %q/%q", event.Event, event.Data)
	}
}

func TestParseStreamEvent(t *testing.T) {
	raw := &RawSSEEvent{
		Event: "content_block_delta",
		Data:  `{"type":"content_block_delta","index":0}`,
	}

	se, err := ParseStreamEvent(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if se.Type != "content_block_delta" {
		t.Errorf("expected type 'content_block_delta', got %q", se.Type)
	}
}
