//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package ycodecli

// Sprint: #323; Story: #1115; Story-ID: a7f947d09851
//
// The terminal side of mid-turn input (turninput.go). The tty stays in
// CANONICAL mode — the kernel echoes and edits the line exactly as it did
// before — with one addition: ESC is an extra line terminator (VEOL), so a
// lone ESC is readable at once instead of waiting for Enter. A read therefore
// returns one complete line (Enter), a line cut by ESC, or a line pushed by
// Ctrl-D; a partly typed line is never returned, so it is never consumed and
// stays in the tty for the shell's line editor when the turn ends.
//
// Readability is polled with select (poll(2) does not work on ttys on macOS)
// at a short interval, so Stop never waits on a blocked read and never takes a
// byte it will not deliver.

import (
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	keyPollInterval = 100 * time.Millisecond
	// escFollowWait is how long the bytes that follow an ESC get to arrive: a
	// key sequence (arrow keys) or a terminal's reply to a query arrives in
	// the same write, a human ESC does not.
	escFollowWait = 40 * time.Millisecond
)

type ttyKeys struct {
	fd     int
	saved  unix.Termios
	owned  unix.Termios
	events chan keyEvent
	quit   chan struct{}
	done   chan struct{}
	once   sync.Once

	// Owned by the reader goroutine until done is closed.
	partial string
	unsent  []keyEvent
}

// openTerminalKeys takes the terminal on f for a turn: f must be a tty in
// canonical mode with this process in its foreground process group.
func openTerminalKeys(f *os.File) (keySource, error) {
	if f == nil {
		return nil, errors.New("no terminal")
	}
	fd := int(f.Fd())
	saved, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	if saved.Lflag&unix.ICANON == 0 {
		return nil, errors.New("terminal is not in canonical mode")
	}
	if foreground, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP); err != nil || foreground != unix.Getpgrp() {
		return nil, errors.New("not the terminal's foreground job")
	}
	k, err := ownTerminal(fd, saved)
	if err != nil {
		return nil, err
	}
	return k, nil
}

// ownTerminal adds ESC as a line terminator and starts reading fd.
func ownTerminal(fd int, saved *unix.Termios) (*ttyKeys, error) {
	owned := *saved
	owned.Cc[unix.VEOL] = 0x1b
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &owned); err != nil {
		return nil, err
	}
	k := &ttyKeys{fd: fd, saved: *saved, owned: owned, events: make(chan keyEvent, 64), quit: make(chan struct{}), done: make(chan struct{})}
	go k.run()
	return k, nil
}

func (k *ttyKeys) Events() <-chan keyEvent { return k.events }

func (k *ttyKeys) Stop() ([]string, string) {
	k.once.Do(func() {
		close(k.quit)
		<-k.done
		_ = unix.IoctlSetTermios(k.fd, ioctlSetTermios, &k.saved)
	})
	var lines []string
	collect := func(ev keyEvent) {
		if !ev.interrupt {
			lines = append(lines, ev.line)
		}
	}
	for _, ev := range k.unsent {
		collect(ev)
	}
	k.unsent = nil
	for {
		select {
		case ev := <-k.events:
			collect(ev)
		default:
			partial := k.partial
			k.partial = ""
			return lines, partial
		}
	}
}

func (k *ttyKeys) run() {
	defer close(k.done)
	buf := make([]byte, 4096)
	for {
		select {
		case <-k.quit:
			return
		default:
		}
		ready, err := readable(k.fd, keyPollInterval)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return
		}
		if !ready {
			continue
		}
		n, err := unix.Read(k.fd, buf)
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			return
		}
		if n == 0 {
			continue // Ctrl-D on an empty line: nothing to deliver
		}
		k.feed(buf[:n])
	}
}

// feed turns one canonical read into events.
func (k *ttyKeys) feed(chunk []byte) {
	text := k.partial + string(chunk)
	k.partial = ""
	switch text[len(text)-1] {
	case '\n':
		k.emit(keyEvent{line: strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r")})
	case 0x1b:
		before := text[:len(text)-1]
		follow := k.readFollow()
		if rest, ok := escapeSequence(follow); ok {
			// A key sequence or a terminal reply, not an interrupt: drop the
			// sequence, keep the typing around it.
			k.partial = before
			k.absorb(rest)
			return
		}
		k.emit(keyEvent{interrupt: true})
		if before != "" {
			// Text typed before the ESC is delivered, not dropped: it
			// reaches the session as a follow-up.
			k.emit(keyEvent{line: before})
		}
		k.absorb(follow)
	default:
		// Ctrl-D pushed a line without Enter: deliver it as typed.
		k.emit(keyEvent{line: text})
	}
}

// absorb splits raw bytes read outside canonical mode into lines and a
// trailing partial.
func (k *ttyKeys) absorb(raw string) {
	raw = strings.ReplaceAll(raw, "\r", "\n")
	for {
		line, rest, found := strings.Cut(raw, "\n")
		if !found {
			k.partial += raw
			return
		}
		k.emit(keyEvent{line: k.partial + line})
		k.partial = ""
		raw = rest
	}
}

// readFollow reads, without canonical processing, whatever arrives right
// after an ESC.
func (k *ttyKeys) readFollow() string {
	raw := k.owned
	raw.Lflag &^= unix.ICANON
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(k.fd, ioctlSetTermios, &raw); err != nil {
		return ""
	}
	defer func() { _ = unix.IoctlSetTermios(k.fd, ioctlSetTermios, &k.owned) }()
	var out []byte
	buf := make([]byte, 256)
	wait := escFollowWait
	for len(out) < 1024 {
		ready, err := readable(k.fd, wait)
		if err != nil || !ready {
			break
		}
		n, err := unix.Read(k.fd, buf)
		if err != nil || n == 0 {
			break
		}
		out = append(out, buf[:n]...)
		wait = 5 * time.Millisecond
	}
	return string(out)
}

// escapeSequence reports whether follow (the bytes after an ESC) starts with
// a CSI or SS3 sequence, and returns what follows the sequence.
func escapeSequence(follow string) (string, bool) {
	if len(follow) < 2 {
		return "", false
	}
	switch follow[0] {
	case 'O':
		return follow[2:], true
	case '[':
		for i := 1; i < len(follow); i++ {
			if c := follow[i]; c >= 0x40 && c <= 0x7e {
				return follow[i+1:], true
			}
		}
	}
	return "", false
}

func (k *ttyKeys) emit(ev keyEvent) {
	if len(k.unsent) > 0 {
		k.unsent = append(k.unsent, ev)
		return
	}
	select {
	case k.events <- ev:
	case <-k.quit:
		k.unsent = append(k.unsent, ev)
	}
}

// readable waits up to timeout for fd to have input.
func readable(fd int, timeout time.Duration) (bool, error) {
	var set unix.FdSet
	set.Set(fd)
	tv := unix.NsecToTimeval(timeout.Nanoseconds())
	n, err := unix.Select(fd+1, &set, nil, nil, &tv)
	if err != nil {
		return false, err
	}
	return n > 0 && set.IsSet(fd), nil
}
