package ycodecli

// Sprint: #387; Story: #27b3dfdf; Story-ID: 27b3dfdf7e76

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/harness/frontend"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// A turn submitted through the web (http) frontend is one session of the
// shared event log: the web resume selector lists it and the terminal UI
// resumes the very same transcript.
func TestWebAndTUIShareSessions(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	workdir := t.TempDir()
	canonical, err := os.ReadFile(harnessFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(workdir, "agent.yaml")
	if err := os.WriteFile(fixture, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", &tuiProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const session = "shared-web-session"
	stream, err := app.Submit(ctx, frontend.Input{FrontendRef: "http", SessionID: session, Principal: "web-user", IdempotencyKey: "web-1", Body: []byte(`{"request":"hello from the web"}`)})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}

	var browser frontend.SessionBrowser = app
	sessions, err := browser.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, s := range sessions {
		listed = listed || s.ID == session
	}
	if !listed {
		t.Fatalf("web selector does not list %q: %+v", session, sessions)
	}
	web, err := browser.Transcript(session)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := (&tuiHost{app: app}).Transcript(session)
	if err != nil {
		t.Fatal(err)
	}
	if len(web) < 2 || len(web) != len(terminal) {
		t.Fatalf("web transcript %+v, terminal transcript %+v", web, terminal)
	}
	for i := range web {
		if web[i].Role != terminal[i].Role || web[i].Text != terminal[i].Text {
			t.Fatalf("entry %d differs: web %+v terminal %+v", i, web[i], terminal[i])
		}
	}
	if !strings.Contains(web[len(web)-2].Text, "hello from the web") || !strings.Contains(web[len(web)-1].Text, "stub-answer-") {
		t.Fatalf("transcript = %+v", web)
	}
}
