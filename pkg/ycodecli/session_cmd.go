package ycodecli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// sessionCLI serves the session operation: read views over the canonical event
// log (list, show, export, search) and the two writes the engine owns (rename
// appends an event; fork derives a child at the latest committed turn).
func sessionCLI(ctx context.Context, app *harnessApplication, inv harnesscli.Invocation, out io.Writer) error {
	h := app.harness
	asJSON := boolFlag(inv, "json")
	pick := func() (public.SessionSummary, error) {
		if len(inv.Arguments) > 0 {
			return h.Session(inv.Arguments[0])
		}
		sessions, err := h.Sessions()
		if err != nil {
			return public.SessionSummary{}, err
		}
		if len(sessions) == 0 {
			return public.SessionSummary{}, errors.New("no sessions yet")
		}
		return sessions[0], nil
	}
	switch inv.Dispatch.Action {
	case "new":
		// Nothing is written to the log until the first turn commits; the
		// old session stays resumable.
		id := uuid.NewString()
		if err := pointSession(id); err != nil {
			return err
		}
		_, err := fmt.Fprintln(out, id)
		return err
	case "status":
		return sessionStatus(app, asJSON, out)
	case "list":
		sessions, err := h.Sessions()
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(out, map[string]any{"schema_version": "ycode-sessions-v1", "sessions": sessions})
		}
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SESSION\tUPDATED\tTURNS\tFAILED\tPARENT\tTITLE")
		for _, s := range sessions {
			parent := "-"
			if s.ParentID != "" {
				parent = shortID(s.ParentID)
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\t%s\n", s.ID, s.Updated.Local().Format(time.DateTime), s.Committed, s.Failed, parent, s.Title)
		}
		return w.Flush()
	case "show", "export":
		s, err := pick()
		if err != nil {
			return err
		}
		transcript, err := h.Transcript(s.ID)
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(out, map[string]any{"schema_version": "ycode-session-v1", "session": s, "messages": transcript})
		}
		if inv.Dispatch.Action == "show" {
			fmt.Fprintf(out, "session  %s\ntitle    %s\nupdated  %s\nturns    %d committed, %d failed\n", s.ID, s.Title, s.Updated.Local().Format(time.DateTime), s.Committed, s.Failed)
			if s.ParentID != "" {
				fmt.Fprintf(out, "parent   %s\n", s.ParentID)
			}
			fmt.Fprintln(out)
		} else {
			fmt.Fprintf(out, "# %s\n\nsession `%s`, updated %s\n\n", orUntitled(s.Title), s.ID, s.Updated.UTC().Format(time.RFC3339))
		}
		for _, m := range transcript {
			text := strings.TrimSpace(public.MessageText(m))
			if text == "" {
				continue
			}
			if inv.Dispatch.Action == "export" {
				fmt.Fprintf(out, "## %s\n\n%s\n\n", m.Role, text)
				continue
			}
			fmt.Fprintf(out, "[%s] %s\n", m.Role, text)
		}
		return nil
	case "rename":
		s, err := h.RenameSession(inv.Arguments[0], strings.Join(inv.Arguments[1:], " "))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s\t%s\n", s.ID, s.Title)
		return err
	case "fork":
		s, err := pick()
		if err != nil {
			return err
		}
		if s.Head == 0 {
			return fmt.Errorf("session %s has no committed turn to fork from", s.ID)
		}
		child := uuid.NewString()
		stream, err := h.Fork(ctx, public.ForkRequest{ParentSessionID: s.ID, SessionID: child, RunID: "fork-" + child, AtSequence: s.Head})
		if err != nil {
			return err
		}
		for range stream {
		}
		_, err = fmt.Fprintln(out, child)
		return err
	case "search":
		matches, err := h.SearchSessions(strings.Join(inv.Arguments, " "))
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(out, map[string]any{"schema_version": "ycode-session-search-v1", "matches": matches})
		}
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		for _, m := range matches {
			fmt.Fprintf(w, "%s\t%s\t%s\n", m.SessionID, m.Role, m.Excerpt)
		}
		return w.Flush()
	}
	return fmt.Errorf("unsupported session action %q", inv.Dispatch.Action)
}

// sessionStatus reports the agent and the session a terminal is on (its
// pointer), else the latest session.
func sessionStatus(app *harnessApplication, asJSON bool, out io.Writer) error {
	h := app.harness
	status := map[string]any{"schema_version": "ycode-status-v1", "agent": app.doc.Metadata.Name, "config": app.doc.Source}
	model, err := defaultHarnessModel(app.doc)
	if err == nil {
		status["model"] = model
	}
	id, pointed := pointedSession(), true
	if id == "" {
		pointed = false
		sessions, err := h.Sessions()
		if err != nil {
			return err
		}
		if len(sessions) > 0 {
			id = sessions[0].ID
		}
	}
	var summary *public.SessionSummary
	if id != "" {
		status["session"] = id
		if s, err := h.Session(id); err == nil {
			summary = &s
			status["summary"] = s
		}
	}
	status["terminal"] = pointed
	if asJSON {
		return writeJSON(out, status)
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "agent\t%s\t%s\n", app.doc.Metadata.Name, app.doc.Source)
	if model != "" {
		fmt.Fprintf(w, "model\t%s\n", model)
	}
	switch {
	case id == "":
		fmt.Fprintln(w, "session\tnone yet")
	case summary == nil:
		fmt.Fprintf(w, "session\t%s\t(new: no turns yet)\n", id)
	default:
		fmt.Fprintf(w, "session\t%s\t%s\n", id, orUntitled(summary.Title))
		fmt.Fprintf(w, "turns\t%d committed, %d failed\n", summary.Committed, summary.Failed)
		fmt.Fprintf(w, "updated\t%s\n", summary.Updated.Local().Format(time.DateTime))
	}
	return w.Flush()
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func orUntitled(title string) string {
	if title == "" {
		return "untitled session"
	}
	return title
}
