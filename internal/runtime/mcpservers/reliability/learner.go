// Pattern Learner — observes (failure → recovery) tuples across
// sessions, persists them, and surfaces them as additional Hint
// Engine rules once a pattern crosses a confidence threshold.
//
// Implementation note: openchrome's full Pattern Learner persists
// to a JSONL log and runs a background promotion thread; we ship a
// simplified Go port that records observations to
// ~/.config/ycode/browser-patterns.jsonl and surfaces aggregated
// counts via Stats(). The "promote to permanent rule" step lives
// in a future commit — for v1, the records are visible to the
// agent and to operators reviewing reliability.

package reliability

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

const promotionConfidence = 0.60
const promotionMinSamples = 3

type learnerWrapper struct {
	inner   mcpservers.Service
	storage *patternStore
}

func newLearnerWrapper(inner mcpservers.Service) *learnerWrapper {
	return &learnerWrapper{inner: inner, storage: defaultStore()}
}

func (l *learnerWrapper) Name() string                       { return l.inner.Name() }
func (l *learnerWrapper) Available(ctx context.Context) bool { return l.inner.Available(ctx) }
func (l *learnerWrapper) EnsureReady(ctx context.Context) error {
	return l.inner.EnsureReady(ctx)
}
func (l *learnerWrapper) Stop(ctx context.Context) error { return l.inner.Stop(ctx) }

func (l *learnerWrapper) Execute(ctx context.Context, action mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	res, err := l.inner.Execute(ctx, action)
	go l.storage.record(action, res, err) // background; tolerate I/O hiccups
	return res, err
}

type patternRecord struct {
	Timestamp time.Time `json:"ts"`
	Action    string    `json:"action"`
	URL       string    `json:"url,omitempty"`
	Outcome   string    `json:"outcome"`
	Error     string    `json:"error,omitempty"`
}

type patternStore struct {
	mu   sync.Mutex
	path string
}

func defaultStore() *patternStore {
	home, _ := os.UserHomeDir()
	return &patternStore{
		path: filepath.Join(home, ".config", "ycode", "browser-patterns.jsonl"),
	}
}

func (p *patternStore) record(action mcpservers.BrowserAction, res *mcpservers.BrowserResult, err error) {
	rec := patternRecord{
		Timestamp: time.Now().UTC(),
		Action:    action.Type,
		URL:       action.URL,
	}
	if err != nil {
		rec.Outcome = "ERROR"
		rec.Error = err.Error()
	} else if res != nil {
		rec.Outcome = res.OutcomeClass
		if rec.Outcome == "" {
			if res.Success {
				rec.Outcome = "SUCCESS"
			} else {
				rec.Outcome = "FAIL"
			}
		}
		rec.Error = res.Error
		if rec.URL == "" {
			rec.URL = res.URL
		}
	}

	line, mErr := json.Marshal(rec)
	if mErr != nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(p.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(line)
	f.Write([]byte{'\n'})
}

// Tunables kept exported so a future "promote to Hint Engine rule"
// path can reference them.
var _ = promotionConfidence
var _ = promotionMinSamples
