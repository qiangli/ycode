package bus

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DeliveryItem is an item in the persistent delivery queue.
type DeliveryItem struct {
	ID        string          `json:"id"`
	EventType EventType       `json:"event_type"`
	SessionID string          `json:"session_id"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
	Attempts  int             `json:"attempts"`
	LastError string          `json:"last_error,omitempty"`
}

// DeliveryFunc is the function called to deliver an item.
// Return nil on success; non-nil triggers retry.
type DeliveryFunc func(item *DeliveryItem) error

// DeliveryQueue is a persistent outbound delivery queue with retry.
type DeliveryQueue struct {
	mu       sync.Mutex
	dir      string
	pending  []*DeliveryItem
	deliver  DeliveryFunc
	maxRetry int
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// DeliveryQueueConfig configures the delivery queue.
type DeliveryQueueConfig struct {
	Dir        string       // persistence directory
	DeliverFn  DeliveryFunc // delivery function
	MaxRetries int          // max attempts per item (default 3)
}

// NewDeliveryQueue creates a persistent delivery queue.
func NewDeliveryQueue(cfg DeliveryQueueConfig) (*DeliveryQueue, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("delivery queue dir required")
	}
	if cfg.DeliverFn == nil {
		return nil, fmt.Errorf("delivery function required")
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("create queue dir: %w", err)
	}
	maxRetry := cfg.MaxRetries
	if maxRetry <= 0 {
		maxRetry = 3
	}

	dq := &DeliveryQueue{
		dir:      cfg.Dir,
		deliver:  cfg.DeliverFn,
		maxRetry: maxRetry,
		stopCh:   make(chan struct{}),
	}

	// Recover pending items from disk.
	if err := dq.recover(); err != nil {
		return nil, fmt.Errorf("recover queue: %w", err)
	}

	return dq, nil
}

// Enqueue adds an item to the queue and persists it.
func (dq *DeliveryQueue) Enqueue(item *DeliveryItem) error {
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}

	dq.mu.Lock()
	dq.pending = append(dq.pending, item)
	dq.mu.Unlock()

	return dq.persistItem(item)
}

// Start begins processing the queue in a background goroutine.
func (dq *DeliveryQueue) Start() {
	dq.wg.Add(1)
	go func() {
		defer dq.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-dq.stopCh:
				// Drain remaining items.
				dq.processAll()
				return
			case <-ticker.C:
				dq.processAll()
			}
		}
	}()
}

// Stop gracefully shuts down the queue, draining pending items.
func (dq *DeliveryQueue) Stop() {
	close(dq.stopCh)
	dq.wg.Wait()
}

// Len returns the number of pending items.
func (dq *DeliveryQueue) Len() int {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	return len(dq.pending)
}

func (dq *DeliveryQueue) processAll() {
	dq.mu.Lock()
	items := make([]*DeliveryItem, len(dq.pending))
	copy(items, dq.pending)
	dq.mu.Unlock()

	var remaining []*DeliveryItem
	for _, item := range items {
		err := dq.deliver(item)
		if err != nil {
			item.Attempts++
			item.LastError = err.Error()
			if item.Attempts < dq.maxRetry {
				remaining = append(remaining, item)
			} else {
				// Max retries exceeded — remove from disk.
				dq.removeItem(item)
			}
		} else {
			dq.removeItem(item)
		}
	}

	dq.mu.Lock()
	dq.pending = remaining
	dq.mu.Unlock()
}

func (dq *DeliveryQueue) persistItem(item *DeliveryItem) error {
	path := filepath.Join(dq.dir, item.ID+".json")
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (dq *DeliveryQueue) removeItem(item *DeliveryItem) {
	path := filepath.Join(dq.dir, item.ID+".json")
	os.Remove(path)
}

func (dq *DeliveryQueue) recover() error {
	entries, err := os.ReadDir(dq.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dq.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var item DeliveryItem
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		dq.pending = append(dq.pending, &item)
	}
	return nil
}

// BackoffDuration returns exponential backoff for a given attempt.
func BackoffDuration(attempt int) time.Duration {
	base := time.Second
	d := time.Duration(math.Pow(2, float64(attempt))) * base
	return min(d, 30*time.Second)
}
