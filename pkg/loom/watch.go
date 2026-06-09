package loom

import (
	"context"
	"sync"
)

// watcherSet is the in-process pub-sub fan-out behind Service.Watch.
// Each subscriber owns a buffered channel and a filter; emit() pushes
// every event through the set, dropping (not blocking) on subscribers
// whose channel is full so a slow consumer cannot wedge the rest of
// the system.
type watcherSet struct {
	mu   sync.Mutex
	next int
	subs map[int]*watcher
}

type watcher struct {
	id     int
	filter WatchFilter
	ch     chan LeaseEvent
}

const (
	// watcherBuffer is the per-subscriber channel buffer. Tuned for the
	// typical case (one consumer reading at near-real-time); a watcher
	// that falls more than `watcherBuffer` events behind loses the
	// overflow rather than blocking the emitter.
	watcherBuffer = 64
)

func newWatcherSet() *watcherSet {
	return &watcherSet{subs: map[int]*watcher{}}
}

// subscribe registers a new watcher and returns its receive-only
// channel. The caller MUST cancel ctx (or call the returned cancel) to
// unsubscribe; the channel is closed at that point.
func (ws *watcherSet) subscribe(ctx context.Context, filter WatchFilter) (<-chan LeaseEvent, context.CancelFunc) {
	w := &watcher{
		filter: filter,
		ch:     make(chan LeaseEvent, watcherBuffer),
	}
	ws.mu.Lock()
	ws.next++
	w.id = ws.next
	ws.subs[w.id] = w
	ws.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-ctx.Done()
		ws.mu.Lock()
		if existing, ok := ws.subs[w.id]; ok {
			delete(ws.subs, w.id)
			close(existing.ch)
		}
		ws.mu.Unlock()
	}()

	return w.ch, cancel
}

// emit fans an event out to every subscriber whose filter matches. A
// non-receiving subscriber drops the event rather than blocking the
// emitter; consumers responsible for keeping up.
func (ws *watcherSet) emit(ev LeaseEvent) {
	ws.mu.Lock()
	subs := make([]*watcher, 0, len(ws.subs))
	for _, w := range ws.subs {
		if w.filter.Matches(ev) {
			subs = append(subs, w)
		}
	}
	ws.mu.Unlock()
	for _, w := range subs {
		select {
		case w.ch <- ev:
		default:
			// Subscriber is behind; drop. The MCP resource handler is
			// expected to track "you missed N events" via its own
			// counter if it cares, not the pub-sub layer.
		}
	}
}

// closeAll terminates every subscription. Called by Service.Close so
// background watchers shut down cleanly with the service.
func (ws *watcherSet) closeAll() {
	ws.mu.Lock()
	for id, w := range ws.subs {
		close(w.ch)
		delete(ws.subs, id)
	}
	ws.mu.Unlock()
}
