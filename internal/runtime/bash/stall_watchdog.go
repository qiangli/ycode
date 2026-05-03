package bash

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// StallWatchdogInterval is how often to poll background task output.
	StallWatchdogInterval = 5 * time.Second

	// StallTailBytes is the number of trailing bytes to check for prompts.
	StallTailBytes = 500

	// MaxTaskOutputBytes is the size at which a background task is killed.
	MaxTaskOutputBytes = 64 * 1024 * 1024
)

// promptPatterns matches interactive prompts that may stall a background task.
var promptPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\[y/n\]`),
	regexp.MustCompile(`(?i)\(yes/no\)`),
	regexp.MustCompile(`(?i)continue\?`),
	regexp.MustCompile(`(?i)press enter`),
	regexp.MustCompile(`(?i)password:`),
	regexp.MustCompile(`(?i)passphrase:`),
	regexp.MustCompile(`(?i)are you sure`),
	regexp.MustCompile(`(?i)overwrite\?`),
	regexp.MustCompile(`(?i)\[Y/n\]`),
	regexp.MustCompile(`(?i)\[y/N\]`),
}

// StallEvent is emitted when a background task appears to be stalled.
type StallEvent struct {
	TaskID        string
	MatchedPrompt string
	OutputTail    string
	TotalBytes    int64
}

// StallWatchdog monitors background task output for interactive prompts
// and excessive output size. When detected, it emits StallEvents.
//
// Inspired by Claude Code's startStallWatchdog() in LocalShellTask.
type StallWatchdog struct {
	mu       sync.Mutex
	handlers []func(StallEvent)
	stopCh   chan struct{}
	stopped  chan struct{}
}

// NewStallWatchdog creates a new stall watchdog.
func NewStallWatchdog() *StallWatchdog {
	return &StallWatchdog{
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// OnStall registers a handler for stall events.
func (sw *StallWatchdog) OnStall(handler func(StallEvent)) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.handlers = append(sw.handlers, handler)
}

// emit sends a stall event to all handlers.
func (sw *StallWatchdog) emit(event StallEvent) {
	sw.mu.Lock()
	handlers := make([]func(StallEvent), len(sw.handlers))
	copy(handlers, sw.handlers)
	sw.mu.Unlock()

	for _, h := range handlers {
		h(event)
	}
}

// Watch starts monitoring the given output function for stalls.
// getOutput should return the current total output bytes and the last N bytes of output.
// killFn is called when output exceeds MaxTaskOutputBytes.
func (sw *StallWatchdog) Watch(taskID string, getOutput func() (totalBytes int64, tail string), killFn func()) {
	go func() {
		defer close(sw.stopped)

		ticker := time.NewTicker(StallWatchdogInterval)
		defer ticker.Stop()

		for {
			select {
			case <-sw.stopCh:
				return
			case <-ticker.C:
				totalBytes, tail := getOutput()

				// Size watchdog: kill if output is too large.
				if totalBytes > MaxTaskOutputBytes {
					sw.emit(StallEvent{
						TaskID:     taskID,
						TotalBytes: totalBytes,
						OutputTail: "output exceeded maximum size limit",
					})
					if killFn != nil {
						killFn()
					}
					return
				}

				// Prompt detection.
				if prompt := DetectInteractivePrompt(tail); prompt != "" {
					sw.emit(StallEvent{
						TaskID:        taskID,
						MatchedPrompt: prompt,
						OutputTail:    tail,
						TotalBytes:    totalBytes,
					})
				}
			}
		}
	}()
}

// Stop stops the watchdog.
func (sw *StallWatchdog) Stop() {
	close(sw.stopCh)
	<-sw.stopped
}

// DetectInteractivePrompt checks if the tail of output contains an
// interactive prompt pattern. Returns the matched pattern or empty string.
func DetectInteractivePrompt(tail string) string {
	// Only check the last few lines.
	lines := strings.Split(tail, "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	lastLines := strings.Join(lines, "\n")

	for _, p := range promptPatterns {
		if loc := p.FindString(lastLines); loc != "" {
			return loc
		}
	}
	return ""
}
