package ycode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
)

// Message is one transcript entry as the session stage stores it.
type Message = message.Message

// SessionSummary is one session projected from the canonical event log. The
// log is the only store: nothing here is cached or written except by
// RenameSession, which appends an event like every other state change.
type SessionSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	ParentID  string    `json:"parent_id,omitempty"`
	Started   time.Time `json:"started"`
	Updated   time.Time `json:"updated"`
	Runs      int       `json:"runs"`
	Committed int       `json:"committed"`
	Failed    int       `json:"failed"`
	// Head is the sequence of the latest committed turn — the point a fork
	// starts from. Zero when no turn has committed yet.
	Head        uint64 `json:"head,omitempty"`
	messagesRef string
}

// SessionMatch is one transcript message that contains a search query.
type SessionMatch struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Index     int    `json:"index"`
	Role      string `json:"role"`
	Excerpt   string `json:"excerpt"`
}

const sessionRenamed = "session.renamed"

// Sessions lists every session in the event log, most recently updated first.
func (h *Harness) Sessions() ([]SessionSummary, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	events, err := event.Replay(h.eventPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	byID := map[string]*SessionSummary{}
	runs := map[string]map[string]bool{}
	var order []string
	for _, item := range events {
		s := byID[item.SessionID]
		if s == nil {
			s = &SessionSummary{ID: item.SessionID, Started: item.Time}
			byID[item.SessionID] = s
			runs[item.SessionID] = map[string]bool{}
			order = append(order, item.SessionID)
		}
		s.Updated = item.Time
		switch item.Type {
		case "input.admitted":
			if !runs[item.SessionID][item.RunID] {
				runs[item.SessionID][item.RunID] = true
				s.Runs++
			}
		case "session.turn-committed":
			var data struct {
				MessagesRef string `json:"messages_ref"`
			}
			if json.Unmarshal(item.Data, &data) == nil && data.MessagesRef != "" {
				s.messagesRef = data.MessagesRef
			}
			s.Committed++
			s.Head = item.Sequence
		case "turn.failed":
			s.Failed++
		case "session.forked":
			var data struct {
				Parent      string `json:"parent_session_id"`
				MessagesRef string `json:"parent_messages_ref"`
			}
			if json.Unmarshal(item.Data, &data) == nil {
				s.ParentID = data.Parent
				if s.messagesRef == "" {
					s.messagesRef = data.MessagesRef
				}
			}
		case sessionRenamed:
			var data struct {
				Title string `json:"title"`
			}
			if json.Unmarshal(item.Data, &data) == nil {
				s.Title = data.Title
			}
		}
	}
	out := make([]SessionSummary, 0, len(order))
	for _, id := range order {
		s := byID[id]
		if s.Title == "" {
			s.Title = h.firstRequest(s.messagesRef)
		}
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// Session returns one session by id or by a unique id prefix.
func (h *Harness) Session(id string) (SessionSummary, error) {
	sessions, err := h.Sessions()
	if err != nil {
		return SessionSummary{}, err
	}
	var found []SessionSummary
	for _, s := range sessions {
		if s.ID == id {
			return s, nil
		}
		if id != "" && strings.HasPrefix(s.ID, id) {
			found = append(found, s)
		}
	}
	switch len(found) {
	case 0:
		return SessionSummary{}, fmt.Errorf("no session %q", id)
	case 1:
		return found[0], nil
	}
	return SessionSummary{}, fmt.Errorf("session prefix %q is ambiguous (%d sessions)", id, len(found))
}

// Transcript returns the session's committed messages (a fork's seed until
// its first turn commits).
func (h *Harness) Transcript(id string) ([]Message, error) {
	s, err := h.Session(id)
	if err != nil {
		return nil, err
	}
	return h.messages(s.messagesRef)
}

// RenameSession records a title for the session.
func (h *Harness) RenameSession(id, title string) (SessionSummary, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return SessionSummary{}, errors.New("session title is empty")
	}
	s, err := h.Session(id)
	if err != nil {
		return SessionSummary{}, err
	}
	if _, err := h.events.Append(event.Draft{SessionID: s.ID, RunID: "session-rename", StageID: "session.rename", Type: sessionRenamed, ConfigDigest: h.doc.ConfigDigest, Data: map[string]string{"title": title}}); err != nil {
		return SessionSummary{}, err
	}
	s.Title = title
	return s, nil
}

// SearchSessions finds transcript text containing query (case-insensitive).
func (h *Harness) SearchSessions(query string) ([]SessionMatch, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("search query is empty")
	}
	sessions, err := h.Sessions()
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(query)
	var out []SessionMatch
	for _, s := range sessions {
		messages, err := h.messages(s.messagesRef)
		if err != nil {
			return nil, err
		}
		for i, m := range messages {
			text := MessageText(m)
			if lower := strings.ToLower(text); len(lower) != len(text) {
				text = lower // keep offsets aligned when case-folding changes byte lengths
			}
			at := strings.Index(strings.ToLower(text), needle)
			if at < 0 {
				continue
			}
			out = append(out, SessionMatch{SessionID: s.ID, Title: s.Title, Index: i, Role: string(m.Role), Excerpt: excerpt(text, at, len(query))})
		}
	}
	return out, nil
}

// MessageText joins a message's text blocks. A user message holds the admitted
// request body verbatim; a body that is a JSON object with one string field
// (the frontend's payload key) reads as that string.
func MessageText(m Message) string {
	var parts []string
	for _, block := range m.Content {
		if block.Type == message.ContentTypeText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if m.Role == message.RoleUser {
		var body map[string]json.RawMessage
		if json.Unmarshal([]byte(text), &body) == nil && len(body) == 1 {
			for _, raw := range body {
				var value string
				if json.Unmarshal(raw, &value) == nil {
					return value
				}
			}
		}
	}
	return text
}

func (h *Harness) messages(ref string) ([]Message, error) {
	if ref == "" {
		return nil, nil
	}
	raw, err := h.payloads.Get(ref)
	if err != nil {
		return nil, err
	}
	var messages []Message
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

// firstRequest is the default title: the first user text, one line.
func (h *Harness) firstRequest(ref string) string {
	messages, err := h.messages(ref)
	if err != nil {
		return ""
	}
	for _, m := range messages {
		if m.Role != message.RoleUser {
			continue
		}
		if text := strings.TrimSpace(MessageText(m)); text != "" {
			line, _, _ := strings.Cut(text, "\n")
			return clip(line, 72)
		}
	}
	return ""
}

func excerpt(text string, at, n int) string {
	start, end := at-40, at+n+40
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	out := strings.Join(strings.Fields(text[start:end]), " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(text) {
		out += "…"
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
