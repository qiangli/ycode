package ycodecli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// SessionFileEnv names the session pointer a terminal front end exports to the
// commands it runs: a file holding the id of the session the terminal is on.
// It is shell state (the exported part): a turn submitted from that terminal
// continues the pointed session, `new` and `resume` move the pointer, and
// `status` reports it. Unset, every command picks its session as before.
const SessionFileEnv = "BASHY_YCODE_SESSION_FILE"

// TerminalSession is what a terminal front end needs to host a session.
type TerminalSession struct {
	Agent   string // the document's metadata name
	Config  string // absolute path of the agent YAML
	Session string // the session the terminal starts on
}

// TerminalUI, when set, hosts the `tui` frontend on a terminal; bashy installs
// its default TUI here. Nil keeps the line reader.
var TerminalUI func(ctx context.Context, s TerminalSession) error

// pointedSession returns the session the enclosing terminal is on, or "" when
// no terminal front end exported a pointer.
func pointedSession() string {
	path := os.Getenv(SessionFileEnv)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// pointSession moves the enclosing terminal's pointer; a no-op without one.
func pointSession(id string) error {
	path := os.Getenv(SessionFileEnv)
	if path == "" || id == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(id+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// insideTerminal reports that this command runs under a terminal front end.
func insideTerminal() bool { return os.Getenv(SessionFileEnv) != "" }

func absSource(path string) (string, error) {
	if path == "" {
		return "", errors.New("the agent document has no source path")
	}
	return filepath.Abs(path)
}
