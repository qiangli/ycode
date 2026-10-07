package ycode

// Sprint: #323; Story: #1115; Story-ID: a7f947d09851
//
// The session queue is the mechanism behind the compiled `spec.queues` policy.
// The YAML already decides everything about mid-turn input: which classes
// exist and their priorities, the capacity and overflow rule, and — in the
// pipelines — where a turn drains which classes (the stock loop drains
// interrupt+steering before every model call and after every tool batch, and
// applies them as user messages). Until now nothing could enqueue, so that
// policy was inert. This file only keeps the items and records every
// transition as an event; it chooses no class, drain point or ordering rule of
// its own beyond the declared discipline.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/qiangli/coreutils/pkg/lockfile"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/turn"
)

// QueueRequest is one input for a session's queue.
type QueueRequest struct {
	SessionID      string
	QueueRef       string
	Class          string // a class declared in the queue's priorities
	Text           string
	IdempotencyKey string
}

// QueuedItem is an item taken back from a queue without a turn draining it.
type QueuedItem struct {
	Class string
	Text  string
}

type queuedItem struct {
	seq      uint64
	class    string
	priority int
	text     string
	key      string
}

type queueKey struct{ queueRef, sessionID string }

// sessionQueue holds pending items per (queue, session). Enqueue and drain
// happen in the process that runs the session's turn; each transition is an
// event in the session's durable log.
type sessionQueue struct {
	path     string
	doc      *spec.Document
	events   *event.Store
	payloads *event.PayloadStore

	mu    sync.Mutex
	seq   uint64
	items map[queueKey][]queuedItem
}

func newSessionQueue(doc *spec.Document, events *event.Store, payloads *event.PayloadStore, path string) *sessionQueue {
	return &sessionQueue{doc: doc, events: events, payloads: payloads, path: path, items: make(map[queueKey][]queuedItem)}
}

// replayLocked reconstructs only versioned durable queue entries. Older
// queue events lacked consumption identities and cannot safely be replayed.
func (q *sessionQueue) replayLocked() error {
	events, err := event.Replay(q.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	q.items = make(map[queueKey][]queuedItem)
	for _, item := range events {
		if item.ConfigDigest != q.doc.ConfigDigest {
			continue
		}
		var data struct {
			QueueRef  string   `json:"queue_ref"`
			Class     string   `json:"class"`
			Ref       string   `json:"payload_ref"`
			Key       string   `json:"idempotency_key"`
			Durable   bool     `json:"durable"`
			Sequences []uint64 `json:"sequences"`
		}
		if item.Type != "queue.enqueued" && item.Type != "queue.consumed" {
			continue
		}
		if err := json.Unmarshal(item.Data, &data); err != nil {
			return err
		}
		key := queueKey{data.QueueRef, item.SessionID}
		if item.Type == "queue.enqueued" && data.Durable {
			raw, err := q.payloads.Get(data.Ref)
			if err != nil {
				return err
			}
			q.items[key] = append(q.items[key], queuedItem{seq: item.Sequence, class: data.Class, priority: q.doc.Spec.Queues[data.QueueRef].Priorities[data.Class], text: string(raw), key: data.Key})
		}
		if item.Type == "queue.consumed" {
			kept := q.items[key][:0]
			for _, entry := range q.items[key] {
				if !slices.Contains(data.Sequences, entry.seq) {
					kept = append(kept, entry)
				}
			}
			q.items[key] = kept
		}
	}
	return nil
}

func (q *sessionQueue) enqueue(request QueueRequest, runID string) error {
	if request.SessionID == "" {
		return errors.New("queue: enqueue requires a session")
	}
	configured, ok := q.doc.Spec.Queues[request.QueueRef]
	if !ok {
		return fmt.Errorf("queue %q is not declared", request.QueueRef)
	}
	priority, ok := configured.Priorities[request.Class]
	if !ok {
		return fmt.Errorf("queue %q declares no class %q", request.QueueRef, request.Class)
	}
	if request.Text == "" {
		return errors.New("queue: item text is empty")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	lock, err := lockfile.Acquire(q.path+".queue.lock", lockfile.Holder{Intent: "session queue admission"})
	if err != nil {
		return err
	}
	defer lock.Release()
	if err := q.replayLocked(); err != nil {
		return err
	}
	key := queueKey{request.QueueRef, request.SessionID}
	if configured.DeduplicateBy == "idempotency-key" && request.IdempotencyKey != "" {
		for _, item := range q.items[key] {
			if item.key == request.IdempotencyKey {
				return nil
			}
		}
	}
	if configured.Capacity > 0 {
		pending := 0
		for k, list := range q.items {
			if k.queueRef == request.QueueRef {
				pending += len(list)
			}
		}
		if pending >= configured.Capacity {
			// reject-new is the only overflow rule implemented; any other
			// declared rule fails closed rather than silently dropping.
			return fmt.Errorf("queue %q is full (%d items, overflow %s)", request.QueueRef, configured.Capacity, configured.Overflow)
		}
	}
	ref, err := q.payloads.Put([]byte(request.Text))
	if err != nil {
		return err
	}
	if _, err := q.events.Append(event.Draft{SessionID: request.SessionID, RunID: runID, StageID: "queue.enqueue", Type: "queue.enqueued", ConfigDigest: q.doc.ConfigDigest, Data: map[string]any{"queue_ref": request.QueueRef, "class": request.Class, "payload_ref": ref, "durable": true, "idempotency_key": request.IdempotencyKey}}); err != nil {
		return err
	}
	q.seq++
	q.items[key] = append(q.items[key], queuedItem{seq: q.seq, class: request.Class, priority: priority, text: request.Text, key: request.IdempotencyKey})
	return nil
}

// take removes the session's items of the given classes (all classes when
// none are named) in priority-fifo order: higher priority first, arrival
// order within a priority.
func (q *sessionQueue) take(sessionID, queueRef string, classes []string) ([]queuedItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	lock, err := lockfile.Acquire(q.path+".queue.lock", lockfile.Holder{Intent: "session queue consumption"})
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	if err := q.replayLocked(); err != nil {
		return nil, err
	}
	key := queueKey{queueRef, sessionID}
	var taken, kept []queuedItem
	for _, item := range q.items[key] {
		if len(classes) == 0 || slices.Contains(classes, item.class) {
			taken = append(taken, item)
		} else {
			kept = append(kept, item)
		}
	}
	if len(taken) > 0 {
		sequences := make([]uint64, len(taken))
		for i, item := range taken {
			sequences[i] = item.seq
		}
		if _, err := q.events.Append(event.Draft{SessionID: sessionID, RunID: uuid.NewString(), StageID: "queue.consume", Type: "queue.consumed", ConfigDigest: q.doc.ConfigDigest, Data: map[string]any{"queue_ref": queueRef, "sequences": sequences}}); err != nil {
			return nil, err
		}
	}
	if len(kept) == 0 {
		delete(q.items, key)
	} else {
		q.items[key] = kept
	}
	sort.SliceStable(taken, func(i, j int) bool {
		if taken[i].priority != taken[j].priority {
			return taken[i].priority > taken[j].priority
		}
		return taken[i].seq < taken[j].seq
	})
	return taken, nil
}

// Drain implements turn.Queue: the queue.drain stage records the drained
// items (queue.drained) itself.
func (q *sessionQueue) Drain(_ context.Context, sessionID, queueRef string, classes []string) ([]turn.QueueItem, error) {
	if _, ok := q.doc.Spec.Queues[queueRef]; !ok {
		return nil, fmt.Errorf("queue %q is not declared", queueRef)
	}
	taken, err := q.take(sessionID, queueRef, classes)
	if err != nil {
		return nil, err
	}
	items := make([]turn.QueueItem, 0, len(taken))
	for _, item := range taken {
		items = append(items, turn.QueueItem{Text: item.text})
	}
	return items, nil
}

// Enqueue adds mid-session input to a session's queue. The compiled
// pipelines decide when a running turn drains it; items no turn drained stay
// pending for the next drain or for TakeQueued.
func (h *Harness) Enqueue(request QueueRequest) error {
	if err := h.Validate(); err != nil {
		return err
	}
	return h.queue.enqueue(request, h.activeRun(request.SessionID))
}

// activeRun names the session's running turn, which a mid-turn item is
// recorded against; with none running the item gets a run id of its own.
func (h *Harness) activeRun(sessionID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	for key := range h.active {
		if session, run, _ := strings.Cut(key, "\x00"); session == sessionID {
			return run
		}
	}
	return uuid.NewString()
}

// TakeQueued removes and returns the session's pending items that no turn
// drained (all classes, priority-fifo), recording the hand-off as a
// queue.taken event. A frontend uses it when a turn ends to submit what was
// queued during it as the session's next input.
func (h *Harness) TakeQueued(sessionID, queueRef string) ([]QueuedItem, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	if _, ok := h.doc.Spec.Queues[queueRef]; !ok {
		return nil, fmt.Errorf("queue %q is not declared", queueRef)
	}
	taken, err := h.queue.take(sessionID, queueRef, nil)
	if err != nil {
		return nil, err
	}
	if len(taken) == 0 {
		return nil, nil
	}
	runID := h.activeRun(sessionID)
	out := make([]QueuedItem, 0, len(taken))
	for _, item := range taken {
		out = append(out, QueuedItem{Class: item.class, Text: item.text})
	}
	if _, err := h.events.Append(event.Draft{SessionID: sessionID, RunID: runID, StageID: "queue.take", Type: "queue.taken", ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"queue_ref": queueRef, "count": len(out)}}); err != nil {
		return out, err
	}
	return out, nil
}

// Settle waits until no run of the session is active: after a frontend
// cancels a turn, the run still records its terminal event and stops its
// in-flight tool. It reports whether the session settled before ctx ended.
func (h *Harness) Settle(ctx context.Context, sessionID string) bool {
	for {
		h.mu.Lock()
		var waiting chan struct{}
		for key, run := range h.active {
			if sessionOf(key) == sessionID {
				waiting = run.done
				break
			}
		}
		h.mu.Unlock()
		if waiting == nil {
			return true
		}
		select {
		case <-waiting:
		case <-ctx.Done():
			return false
		}
	}
}
