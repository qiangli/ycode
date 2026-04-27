package task

import (
	"fmt"
	"time"
)

// TaskMessage is a message exchanged between tasks/agents via their mailbox.
type TaskMessage struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Type    string `json:"type"` // "result", "progress", "request", "cancel"
	Payload string `json:"payload"`
}

// Mailbox provides buffered message passing for a task node.
type Mailbox struct {
	ch chan TaskMessage
}

// NewMailbox creates a mailbox with the given buffer size.
func NewMailbox(bufferSize int) *Mailbox {
	if bufferSize <= 0 {
		bufferSize = 16
	}
	return &Mailbox{
		ch: make(chan TaskMessage, bufferSize),
	}
}

// Send sends a message to this mailbox. Returns an error if the mailbox is full.
func (m *Mailbox) Send(msg TaskMessage) error {
	select {
	case m.ch <- msg:
		return nil
	default:
		return fmt.Errorf("mailbox full (capacity %d)", cap(m.ch))
	}
}

// Receive waits for a message with the given timeout.
// Returns the message and true, or zero value and false on timeout.
func (m *Mailbox) Receive(timeout time.Duration) (TaskMessage, bool) {
	if timeout <= 0 {
		select {
		case msg := <-m.ch:
			return msg, true
		default:
			return TaskMessage{}, false
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case msg := <-m.ch:
		return msg, true
	case <-timer.C:
		return TaskMessage{}, false
	}
}

// Pending returns the number of messages waiting in the mailbox.
func (m *Mailbox) Pending() int {
	return len(m.ch)
}

// Drain reads and discards all pending messages, returning the count.
func (m *Mailbox) Drain() int {
	count := 0
	for {
		select {
		case <-m.ch:
			count++
		default:
			return count
		}
	}
}
