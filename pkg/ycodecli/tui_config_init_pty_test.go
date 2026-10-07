//go:build !windows

package ycodecli

// Sprint: #387; Story: #1625; Story-ID: 27b3dfdf7e76

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty/v2"

	public "github.com/qiangli/ycode/pkg/ycode"
)

// instructionFixture is examples/agent.yaml plus what genie's YAML declares
// for /init: a workspace GENIE.md source loaded by the coding context, a
// template carrying a marker the stub model reports, and the init command.
func instructionFixture(t *testing.T, dir, file, name string) string {
	t.Helper()
	canonical, err := os.ReadFile(harnessFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(canonical)
	for _, edit := range [][2]string{
		{"metadata:\n  name: ycode\n", "metadata:\n  name: " + name + "\n"},
		{"          - {name: version,", "          - {name: init, short: Create or use GENIE.md, args: {min: 0, max: 0}, dispatch: {operation: init, sourceRef: repository-instructions, templateRef: repository-instructions-template}}\n          - {name: version,"},
		{"\n  sources:\n", "\n  sources:\n    repository-instructions:\n      file: {path: GENIE.md, base: workspace, required: false}\n      limits: {maxBytes: 65536}\n    repository-instructions-template:\n      text: \"GENIE-MARKER-387: follow the repository rules.\\n\"\n      limits: {maxBytes: 4096}\n"},
		{"        - id: project\n          sourceRef: project-instructions\n          role: system\n          cache: {scope: workspace, breakAfter: true}\n", "        - id: project\n          sourceRef: project-instructions\n          role: system\n          cache: {scope: workspace, breakAfter: true}\n        - id: repository\n          sourceRef: repository-instructions\n          role: system\n          cache: {scope: workspace, breakAfter: true}\n"},
	} {
		if strings.Count(text, edit[0]) != 1 {
			t.Fatalf("fixture edit anchor %q is not unique", edit[0])
		}
		text = strings.Replace(text, edit[0], edit[1], 1)
	}
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type ptyRun struct {
	t       *testing.T
	command *exec.Cmd
	term    *os.File
	screen  *ptyScreen
}

func startTUIOverPTY(t *testing.T, workdir, home, fixture, args string) *ptyRun {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestTUIOverPTY$")
	command.Dir = workdir
	command.Env = append(os.Environ(), "YCODE_TUI_PTY_HELPER=1", "YCODE_TUI_FIXTURE="+fixture, "YCODE_TUI_ARGS="+args, "HOME="+home, "BASHY_HOME=", "OPENAI_API_KEY=test-secret", "TERM=xterm-256color", SessionFileEnv+"=")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 40, Cols: 160})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = terminal.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	})
	run := &ptyRun{t: t, command: command, term: terminal, screen: &ptyScreen{}}
	go func() { _, _ = io.Copy(run.screen, terminal) }()
	return run
}

func (r *ptyRun) expect(step, want string) {
	r.t.Helper()
	if !r.screen.waitFor(want, 15*time.Second) {
		r.t.Fatalf("%s: %q never appeared; screen:\n%s", step, want, r.screen.text())
	}
}

func (r *ptyRun) expectCount(step, want string, n int) {
	r.t.Helper()
	if !r.screen.waitCount(want, n, 15*time.Second) {
		r.t.Fatalf("%s: %q did not appear %d times; screen:\n%s", step, want, n, r.screen.text())
	}
}

func (r *ptyRun) line(text string) {
	r.t.Helper()
	if _, err := io.WriteString(r.term, text+"\r"); err != nil {
		r.t.Fatal(err)
	}
}

func (r *ptyRun) quit() {
	r.t.Helper()
	r.line("/quit")
	r.expect("helper exit", "TUI-EXIT ok")
	done := make(chan error, 1)
	go func() { done <- r.command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			r.t.Fatalf("helper: %v\n%s", err, r.screen.text())
		}
	case <-time.After(10 * time.Second):
		r.t.Fatalf("helper did not exit after /quit:\n%s", r.screen.text())
	}
}

var greetingSession = regexp.MustCompile(`session ([0-9a-f-]{36}) —`)

// TestTUIConfigAndInitOverPTY is the A7 /config and /init acceptance: a
// fresh terminal reports its configuration source, /init seeds GENIE.md and
// its instructions reach the next turn, other agents' files and an existing
// GENIE.md stay byte-identical, an invalid custom file is refused and a valid
// one is used; a second terminal then resumes the saved session.
func TestTUIConfigAndInitOverPTY(t *testing.T) {
	home := t.TempDir()
	// The helpers and this process share one control root under HOME.
	t.Setenv("HOME", home)
	t.Setenv("BASHY_HOME", "")
	t.Setenv("OPENAI_API_KEY", "test-secret")
	workdir := t.TempDir()
	fixture := instructionFixture(t, workdir, "agent.yaml", "genie-fixture")
	custom := instructionFixture(t, workdir, "custom.yaml", "custom-fixture")
	others := map[string]string{"AGENTS.md": "agents: keep me\n", "CLAUDE.md": "claude: keep me\n", "GEMINI.md": "gemini: keep me\n"}
	for name, body := range others {
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workdir, "bad.yaml"), []byte("kind: NotAHarness\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	genie := filepath.Join(workdir, "GENIE.md")

	// A fresh terminal and session.
	first := startTUIOverPTY(t, workdir, home, fixture, "")
	first.expect("greeting", "Ctrl-D leaves.")
	match := greetingSession.FindStringSubmatch(first.screen.text())
	if match == nil {
		t.Fatalf("no session in the greeting:\n%s", first.screen.text())
	}
	saved := match[1]

	first.line("/help")
	first.expect("/help lists /config", "config source | config use FILE")

	first.line("/config")
	first.expect("/config reports the effective file", "config: "+fixture)
	first.expect("/config reports its origin", "origin: flag")
	first.expect("/config names the configuration", "name: genie-fixture")

	first.line("/init")
	first.expect("/init creates GENIE.md", "created: ")
	first.expect("/init names the context that loads it", "loaded by coding/repository")
	first.expect("/init reloads the configuration", "from the next turn")
	if strings.Contains(first.screen.text(), "new session") {
		t.Fatalf("/init on a session with no turn moved sessions:\n%s", first.screen.text())
	}
	if got, err := os.ReadFile(genie); err != nil || !strings.Contains(string(got), "GENIE-MARKER-387") {
		t.Fatalf("GENIE.md = %q, %v", got, err)
	}
	first.line("what are the rules?")
	first.expect("GENIE.md reaches the model", "instructions-in-context=true")
	first.expectCount("turn end", "turn ended in", 1)

	// An existing GENIE.md is the author's: /init uses it unchanged, and
	// the recompiled context carries the authored text, not the template.
	authored := "# my own rules\nnever touch vendor/\n"
	if err := os.WriteFile(genie, []byte(authored), 0o644); err != nil {
		t.Fatal(err)
	}
	first.line("/init")
	first.expect("/init keeps an existing GENIE.md", "left unchanged")
	first.expect("a changed configuration moves a session with turns", "stays resumable with /resume "+saved)
	if got, _ := os.ReadFile(genie); string(got) != authored {
		t.Fatalf("/init overwrote GENIE.md: %q", got)
	}
	first.line("and now, the rules?")
	first.expect("the authored GENIE.md is the context", "instructions-in-context=false")
	first.expectCount("turn end", "turn ended in", 2)

	// A configuration switch never happens under a running turn.
	first.line("please be slow")
	first.expect("slow turn running", "esc stops")
	first.line("/config " + custom)
	first.expect("/config refused mid-turn", "/config refused during a turn")
	if _, err := io.WriteString(first.term, "\x1b"); err != nil {
		t.Fatal(err)
	}
	first.expect("ESC", "interrupted")

	// Fail closed: an invalid custom file is not used.
	first.line("/config bad.yaml")
	first.expect("/config refuses an invalid file", "is not used")
	first.line("/config")
	first.expectCount("still the original configuration", "name: genie-fixture", 2)

	first.line("/config " + custom)
	first.expect("/config switches to the custom file", "configuration: "+custom)
	first.line("/config")
	first.expect("/config reports the terminal override", "origin: /config in this terminal")
	first.expect("/config reports the custom configuration", "name: custom-fixture")
	first.line("hello custom")
	first.expect("turn on the custom configuration", "stub-answer-")
	first.expectCount("custom turn end", "turn ended in", 3)
	first.quit()

	for name, body := range others {
		if got, err := os.ReadFile(filepath.Join(workdir, name)); err != nil || string(got) != body {
			t.Fatalf("%s changed: %q, %v", name, got, err)
		}
	}

	// Save/resume compatibility: a second terminal resumes the first
	// session from the event log and continues it.
	second := startTUIOverPTY(t, workdir, home, fixture, "resume "+saved)
	second.expect("resumed greeting", "session "+saved)
	second.line("/save")
	second.expect("/save keeps the resumed session", "saved: session "+saved)
	second.line("again, the rules?")
	second.expect("resumed turn", "instructions-in-context=false")
	second.expectCount("resumed turn end", "turn ended in", 1)
	second.quit()

	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", &tuiProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	entries, err := app.Transcript(saved)
	if err != nil {
		t.Fatal(err)
	}
	var users []string
	for _, e := range entries {
		if e.Role == "user" {
			users = append(users, e.Text)
		}
	}
	if len(users) != 2 || !strings.Contains(users[0], "what are the rules?") || !strings.Contains(users[1], "again, the rules?") {
		t.Fatalf("resumed session transcript users = %q", users)
	}
}
