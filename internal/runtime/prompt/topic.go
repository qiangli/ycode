package prompt

import (
	"strings"
	"sync"
)

const (
	// TopicMaxTurns is how many turns without update before the topic is cleared.
	TopicMaxTurns = 20
	// topicMaxLen is the maximum length of a topic string.
	topicMaxLen = 120
)

// TopicTracker extracts and maintains the current high-level task focus
// from user messages. The active topic is injected into the system prompt
// as a lightweight focus signal.
type TopicTracker struct {
	mu               sync.Mutex
	currentTopic     string
	turnsSinceUpdate int
}

// NewTopicTracker creates a new tracker with no active topic.
func NewTopicTracker() *TopicTracker {
	return &TopicTracker{}
}

// Update processes a user message and potentially updates the active topic.
// Only updates if the message appears to signal a task change.
func (t *TopicTracker) Update(userMessage string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.turnsSinceUpdate++

	// Clear stale topic.
	if t.turnsSinceUpdate > TopicMaxTurns {
		t.currentTopic = ""
		return
	}

	// Check for explicit topic markers.
	topic := extractTopic(userMessage)
	if topic != "" {
		t.currentTopic = truncateString(topic, topicMaxLen)
		t.turnsSinceUpdate = 0
	}
}

// Inject returns the topic injection string for the system prompt.
// Returns empty string if no topic is active.
func (t *TopicTracker) Inject() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.currentTopic == "" {
		return ""
	}
	return "[Active Topic: " + t.currentTopic + "]"
}

// CurrentTopic returns the current topic string (may be empty).
func (t *TopicTracker) CurrentTopic() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.currentTopic
}

// SetTopic explicitly sets the topic (e.g., from a state snapshot).
func (t *TopicTracker) SetTopic(topic string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentTopic = truncateString(topic, topicMaxLen)
	t.turnsSinceUpdate = 0
}

// extractTopic identifies topic-changing patterns in user messages.
func extractTopic(msg string) string {
	// Check for explicit markers first.
	markers := []string{
		"let's work on ",
		"now i want to ",
		"switch to ",
		"focus on ",
		"help me with ",
		"i need to ",
		"please implement ",
		"please add ",
		"please fix ",
		"can you ",
	}

	lower := strings.ToLower(strings.TrimSpace(msg))
	for _, marker := range markers {
		if idx := strings.Index(lower, marker); idx >= 0 {
			rest := msg[idx+len(marker):]
			return extractFirstSentence(rest)
		}
	}

	// For short messages (likely a new task directive), use the first sentence.
	if len(msg) < 200 && !isFollowUp(lower) {
		return extractFirstSentence(msg)
	}

	return ""
}

// extractFirstSentence returns text up to the first sentence boundary.
func extractFirstSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Find first sentence-ending punctuation or newline.
	for i, r := range text {
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			result := strings.TrimSpace(text[:i])
			if len(result) > 0 {
				return result
			}
		}
	}

	// No sentence boundary found — use the whole text if short.
	if len(text) <= topicMaxLen {
		return text
	}
	return text[:topicMaxLen]
}

// isFollowUp detects messages that are continuations, not new tasks.
func isFollowUp(lower string) bool {
	followUpPrefixes := []string{
		"yes", "no", "ok", "sure", "thanks", "thank you",
		"great", "good", "perfect", "looks good",
		"what about", "how about", "also",
		"and ", "but ", "wait",
	}
	for _, prefix := range followUpPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}
