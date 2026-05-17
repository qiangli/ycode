package detector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Sink writes FailureSignals somewhere persistent. Phase 1 ships
// JSONLineSink (append-only file at
// ~/.agents/ycode/selfheal/observations.jsonl). Phase 3 will add a
// BacklogSink that writes a markdown frontmatter entry; both can be
// composed via MultiSink.
type Sink interface {
	Record(sig FailureSignal) error
	Close() error
}

// JSONLineSink writes one JSON object per line. Safe for concurrent
// callers; uses an in-process mutex rather than O_APPEND alone so
// long signal payloads don't interleave on platforms where atomic
// append is only guaranteed up to PIPE_BUF.
type JSONLineSink struct {
	mu sync.Mutex
	f  *os.File
}

// NewJSONLineSink opens path for append (creating parent dirs as
// needed) and returns a ready-to-use sink. The path is created with
// 0o600 perms — selfheal observations carry tool inputs that may
// include user prompts; same secrecy posture as session logs.
func NewJSONLineSink(path string) (*JSONLineSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("selfheal: mkdir sink dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("selfheal: open sink: %w", err)
	}
	return &JSONLineSink{f: f}, nil
}

func (s *JSONLineSink) Record(sig FailureSignal) error {
	b, err := json.Marshal(sig)
	if err != nil {
		return fmt.Errorf("selfheal: marshal signal: %w", err)
	}
	b = append(b, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.f.Write(b)
	return err
}

func (s *JSONLineSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}
