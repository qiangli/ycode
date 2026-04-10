package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SSEParser reads Server-Sent Events from a reader and emits parsed events.
type SSEParser struct {
	reader *bufio.Reader
}

// NewSSEParser creates a new SSE parser from a reader.
func NewSSEParser(r io.Reader) *SSEParser {
	return &SSEParser{reader: bufio.NewReader(r)}
}

// RawSSEEvent is an unparsed SSE event with event type and data.
type RawSSEEvent struct {
	Event string
	Data  string
}

// Next reads the next SSE event from the stream.
// Returns io.EOF when the stream ends.
func (p *SSEParser) Next() (*RawSSEEvent, error) {
	var event RawSSEEvent
	var dataLines []string

	for {
		line, err := p.reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")

		if err != nil {
			// If we have accumulated data, return it before the error.
			if len(dataLines) > 0 {
				event.Data = strings.Join(dataLines, "\n")
				return &event, nil
			}
			return nil, err
		}

		// Empty line means end of event.
		if line == "" {
			if len(dataLines) > 0 || event.Event != "" {
				event.Data = strings.Join(dataLines, "\n")
				return &event, nil
			}
			continue
		}

		// Parse field.
		if strings.HasPrefix(line, "event: ") {
			event.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
		} else if line == "event:" {
			event.Event = ""
		} else if strings.HasPrefix(line, ":") {
			// Comment line, skip.
			continue
		}
	}
}

// ParseStreamEvent parses a raw SSE event into a typed StreamEvent.
func ParseStreamEvent(raw *RawSSEEvent) (*StreamEvent, error) {
	if raw.Data == "" {
		return &StreamEvent{Type: raw.Event}, nil
	}

	var se StreamEvent
	if err := json.Unmarshal([]byte(raw.Data), &se); err != nil {
		return nil, fmt.Errorf("parse stream event data: %w", err)
	}
	se.Type = raw.Event
	return &se, nil
}
