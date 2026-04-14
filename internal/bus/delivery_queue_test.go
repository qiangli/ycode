package bus

import (
	"fmt"
	"testing"
	"time"
)

func TestDeliveryQueue_EnqueueAndProcess(t *testing.T) {
	dir := t.TempDir()

	var delivered []*DeliveryItem
	dq, err := NewDeliveryQueue(DeliveryQueueConfig{
		Dir: dir,
		DeliverFn: func(item *DeliveryItem) error {
			delivered = append(delivered, item)
			return nil
		},
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := dq.Enqueue(&DeliveryItem{
		ID:        "item-1",
		EventType: EventTurnComplete,
		SessionID: "sess-1",
		Data:      []byte(`{"status":"complete"}`),
	}); err != nil {
		t.Fatal(err)
	}

	if dq.Len() != 1 {
		t.Errorf("expected 1 pending, got %d", dq.Len())
	}

	dq.processAll()

	if len(delivered) != 1 {
		t.Fatalf("expected 1 delivered, got %d", len(delivered))
	}
	if delivered[0].ID != "item-1" {
		t.Errorf("expected item-1, got %s", delivered[0].ID)
	}
	if dq.Len() != 0 {
		t.Errorf("expected 0 pending after delivery, got %d", dq.Len())
	}
}

func TestDeliveryQueue_RetryOnFailure(t *testing.T) {
	dir := t.TempDir()

	attempts := 0
	dq, err := NewDeliveryQueue(DeliveryQueueConfig{
		Dir: dir,
		DeliverFn: func(item *DeliveryItem) error {
			attempts++
			if attempts < 3 {
				return fmt.Errorf("transient error")
			}
			return nil
		},
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	dq.Enqueue(&DeliveryItem{ID: "retry-item", EventType: EventTurnComplete, Data: []byte(`{}`)})

	// Process until delivered or exhausted.
	for range 5 {
		dq.processAll()
		if dq.Len() == 0 {
			break
		}
	}

	if attempts < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts)
	}
}

func TestDeliveryQueue_MaxRetriesExceeded(t *testing.T) {
	dir := t.TempDir()

	dq, err := NewDeliveryQueue(DeliveryQueueConfig{
		Dir: dir,
		DeliverFn: func(item *DeliveryItem) error {
			return fmt.Errorf("always fails")
		},
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	dq.Enqueue(&DeliveryItem{ID: "fail-item", EventType: EventTurnComplete, Data: []byte(`{}`)})

	for range 5 {
		dq.processAll()
	}

	if dq.Len() != 0 {
		t.Errorf("expected item removed after max retries, got %d pending", dq.Len())
	}
}

func TestDeliveryQueue_Recovery(t *testing.T) {
	dir := t.TempDir()

	// Create first queue and enqueue without processing.
	dq1, _ := NewDeliveryQueue(DeliveryQueueConfig{
		Dir:       dir,
		DeliverFn: func(item *DeliveryItem) error { return nil },
	})
	dq1.Enqueue(&DeliveryItem{ID: "recover-item", EventType: EventTurnComplete, Data: []byte(`{}`)})

	// Create second queue from same dir — should recover the item.
	deliverCalled := false
	dq2, _ := NewDeliveryQueue(DeliveryQueueConfig{
		Dir: dir,
		DeliverFn: func(item *DeliveryItem) error {
			deliverCalled = true
			return nil
		},
	})

	if dq2.Len() != 1 {
		t.Errorf("expected 1 recovered item, got %d", dq2.Len())
	}

	dq2.processAll()
	if !deliverCalled {
		t.Error("recovered item should have been delivered")
	}
}

func TestBackoffDuration(t *testing.T) {
	if d := BackoffDuration(0); d != time.Second {
		t.Errorf("attempt 0: expected 1s, got %v", d)
	}
	if d := BackoffDuration(1); d != 2*time.Second {
		t.Errorf("attempt 1: expected 2s, got %v", d)
	}
	// Should cap at 30s.
	if d := BackoffDuration(10); d != 30*time.Second {
		t.Errorf("attempt 10: expected 30s cap, got %v", d)
	}
}
