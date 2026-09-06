//go:build !windows

package frontend

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty/v2"
	"github.com/qiangli/ycode/internal/harness/event"
)

// TestPTYREPLProjectsCanonicalEvents launches this test binary behind a real
// pseudo-terminal. The PTY owns bytes and terminal lifecycle only; the same
// neutral REPL adapter still owns canonical input and event projection.
func TestPTYREPLProjectsCanonicalEvents(t *testing.T) {
	if os.Getenv("YCODE_HARNESS_PTY_HELPER") == "1" {
		doc := localSurfaceDocument()
		controller := &fakeController{events: []event.Event{{Sequence: 1, Type: "turn.completed"}}}
		repl, err := NewREPL(doc, "repl", controller)
		if err != nil {
			t.Fatal(err)
		}
		err = repl.Run(context.Background(), os.Stdin, Defaults{SessionID: "pty", AgentRef: "coder"}, RenderFunc(func(item event.Event) error {
			_, writeErr := io.WriteString(os.Stdout, "EVENT:"+item.Type+"\n")
			return writeErr
		}))
		if err != nil {
			t.Fatal(err)
		}
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestPTYREPLProjectsCanonicalEvents$")
	command.Env = append(os.Environ(), "YCODE_HARNESS_PTY_HELPER=1")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = terminal.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	})
	if _, err := io.WriteString(terminal, "hello\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var output bytes.Buffer
	buffer := make([]byte, 1024)
	for time.Now().Before(deadline) && !strings.Contains(output.String(), "EVENT:turn.completed") {
		_ = terminal.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		if count, _ := terminal.Read(buffer); count > 0 {
			output.Write(buffer[:count])
		}
	}
	if !strings.Contains(output.String(), "EVENT:turn.completed") {
		t.Fatalf("PTY did not project canonical completion event: %q", output.String())
	}
}
