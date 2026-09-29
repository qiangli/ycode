//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package ycodecli

// Sprint: #323; Story: #1115; Story-ID: a7f947d09851

import (
	"os"
	"testing"
	"time"

	"github.com/creack/pty/v2"
	"golang.org/x/sys/unix"
)

// ownedPTY opens a pty pair, owns its terminal side like a turn does, and
// returns the keyboard (the master) and the owner.
func ownedPTY(t *testing.T) (*os.File, *os.File, *ttyKeys, unix.Termios) {
	t.Helper()
	master, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { _ = master.Close(); _ = tty.Close() })
	saved, err := unix.IoctlGetTermios(int(tty.Fd()), ioctlGetTermios)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Lflag&unix.ICANON == 0 {
		t.Fatalf("a fresh pty is not canonical")
	}
	keys, err := ownTerminal(int(tty.Fd()), saved)
	if err != nil {
		t.Fatal(err)
	}
	return master, tty, keys, *saved
}

func nextEvent(t *testing.T, keys *ttyKeys) keyEvent {
	t.Helper()
	select {
	case ev := <-keys.Events():
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("no key event")
		return keyEvent{}
	}
}

func noEvent(t *testing.T, keys *ttyKeys, wait time.Duration) {
	t.Helper()
	select {
	case ev := <-keys.Events():
		t.Fatalf("unexpected key event %#v", ev)
	case <-time.After(wait):
	}
}

// Enter delivers a line; a lone ESC interrupts at once (no Enter needed); an
// arrow key's ESC sequence does not.
func TestTerminalKeysLinesAndESC(t *testing.T) {
	master, _, keys, _ := ownedPTY(t)
	defer keys.Stop()
	if _, err := master.Write([]byte("STOP: a parallel lane owns this\r")); err != nil {
		t.Fatal(err)
	}
	if ev := nextEvent(t, keys); ev.interrupt || ev.line != "STOP: a parallel lane owns this" {
		t.Fatalf("event = %#v, want the line", ev)
	}
	if _, err := master.Write([]byte{0x1b}); err != nil {
		t.Fatal(err)
	}
	if ev := nextEvent(t, keys); !ev.interrupt {
		t.Fatalf("event = %#v, want the ESC interrupt", ev)
	}
	if _, err := master.Write([]byte("\x1b[A")); err != nil {
		t.Fatal(err)
	}
	noEvent(t, keys, 300*time.Millisecond)
	if _, err := master.Write([]byte("\r")); err != nil {
		t.Fatal(err)
	}
	if ev := nextEvent(t, keys); ev.interrupt || ev.line != "" {
		t.Fatalf("event = %#v, want an empty line (the arrow key dropped)", ev)
	}
}

// A partly typed line is never read by the turn: after Stop it is still in
// the terminal, whole, for the shell's line editor; the terminal settings
// are restored.
func TestTerminalKeysNeverStealPartialLine(t *testing.T) {
	master, tty, keys, saved := ownedPTY(t)
	if _, err := master.Write([]byte("half-typed")); err != nil {
		t.Fatal(err)
	}
	noEvent(t, keys, 300*time.Millisecond)
	lines, partial := keys.Stop()
	if len(lines) != 0 || partial != "" {
		t.Fatalf("Stop returned lines=%q partial=%q; the partial line must stay in the tty", lines, partial)
	}
	restored, err := unix.IoctlGetTermios(int(tty.Fd()), ioctlGetTermios)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Cc[unix.VEOL] != saved.Cc[unix.VEOL] || restored.Lflag != saved.Lflag {
		t.Fatalf("terminal not restored: VEOL %#x want %#x", restored.Cc[unix.VEOL], saved.Cc[unix.VEOL])
	}
	if _, err := master.Write([]byte("\r")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	_ = tty.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := tty.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "half-typed\n" {
		t.Fatalf("the shell would read %q, want the whole partial line", got)
	}
}
