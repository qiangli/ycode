package agentmode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/shell"
)

// Record is one row in the JSONL mining sink. Each Suggest/SuggestPost
// call appends a Record so we can later see which commands the catalog
// missed and what the actual hint hit-rate looks like.
type Record struct {
	TS       time.Time `json:"ts"`
	Phase    string    `json:"phase"` // "pre" or "post"
	Cmd      string    `json:"cmd,omitempty"`
	ExitCode int       `json:"exit_code,omitempty"`
	FiredIDs []string  `json:"fired_ids,omitempty"`
}

// envHistoryFile names the env var that overrides the JSONL path.
// envMineDisable, when set to "1", short-circuits all sink writes —
// used by tests so unit runs never touch the user's real history file.
const (
	envHistoryFile = "YCODE_SHELL_HISTORY_FILE"
	envMineDisable = "YCODE_SHELL_MINE_DISABLE"
)

// HistoryPath returns the resolved JSONL sink path. Exposed so the
// CLI's --mine subcommand can read the same file the sink writes.
func HistoryPath() string {
	if p := os.Getenv(envHistoryFile); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agents", "ycode", "shell-history.jsonl")
}

// sinkMu serializes appends so concurrent Suggest calls don't interleave
// partial JSON lines. The lock is held only for the duration of one
// append (open + write + close); contention on a per-command write is
// not the bottleneck.
var sinkMu sync.Mutex

// RecordPre logs a pre-execution hint match. firedIDs may be empty for
// commands that produced no hint — those are exactly the rows the
// catalog-improvement loop wants to surface.
func RecordPre(cmd string, firedIDs []string) {
	record(Record{
		TS:       time.Now().UTC(),
		Phase:    "pre",
		Cmd:      cmd,
		FiredIDs: firedIDs,
	})
}

// RecordPost logs a post-execution hint match. exitCode is the dispatch
// result's exit code; firedIDs are the post-catalog hint IDs that
// matched.
func RecordPost(exitCode int, firedIDs []string) {
	record(Record{
		TS:       time.Now().UTC(),
		Phase:    "post",
		ExitCode: exitCode,
		FiredIDs: firedIDs,
	})
}

func record(r Record) {
	if os.Getenv(envMineDisable) == "1" {
		shell.ObserveMineWrite(r.Phase, "disabled")
		return
	}
	path := HistoryPath()
	if path == "" {
		shell.ObserveMineWrite(r.Phase, "no_path")
		return
	}
	sinkMu.Lock()
	defer sinkMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		shell.ObserveMineWrite(r.Phase, "mkdir_err")
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		shell.ObserveMineWrite(r.Phase, "open_err")
		return
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(r); err != nil {
		shell.ObserveMineWrite(r.Phase, "encode_err")
		return
	}
	shell.ObserveMineWrite(r.Phase, "ok")
}
