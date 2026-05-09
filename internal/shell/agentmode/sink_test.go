package agentmode

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSinkRecordPreWritesJSONL pins the on-disk format the mining
// analyzer expects.
func TestSinkRecordPreWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h.jsonl")
	t.Setenv(envHistoryFile, path)
	t.Setenv(envMineDisable, "")

	RecordPre("git status", []string{"git-log-status-diff-suggests-yc-git"})
	RecordPre("ssomething --weird", nil)
	RecordPost(0, []string{"exit-127-suggests-yc-help"})

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var got []Record
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var r Record
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatalf("decode: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 records, got %d: %+v", len(got), got)
	}
	if got[0].Phase != "pre" || got[0].Cmd != "git status" || len(got[0].FiredIDs) != 1 {
		t.Errorf("pre[0] mismatch: %+v", got[0])
	}
	if got[1].Phase != "pre" || got[1].Cmd != "ssomething --weird" || len(got[1].FiredIDs) != 0 {
		t.Errorf("pre[1] (miss) mismatch: %+v", got[1])
	}
	if got[2].Phase != "post" || len(got[2].FiredIDs) != 1 {
		t.Errorf("post[0] mismatch: %+v", got[2])
	}
}

// TestSinkDisableKnobShortCircuits verifies the disable env wins so test
// runs never pollute the user's real history file.
func TestSinkDisableKnobShortCircuits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h.jsonl")
	t.Setenv(envHistoryFile, path)
	t.Setenv(envMineDisable, "1")

	RecordPre("git status", []string{"x"})
	RecordPost(0, []string{"y"})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		data, _ := os.ReadFile(path)
		t.Fatalf("disable knob ignored: file exists with %q", data)
	}
}

// TestSinkAutoCreatesDirectory keeps the sink working on the first run
// after a fresh ycode install (when ~/.agents/ycode doesn't exist yet).
func TestSinkAutoCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "subdir", "h.jsonl")
	t.Setenv(envHistoryFile, path)
	t.Setenv(envMineDisable, "")

	RecordPre("anything", nil)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), `"phase":"pre"`) {
		t.Fatalf("payload not written: %q", data)
	}
}
