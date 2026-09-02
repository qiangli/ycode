package event

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreAppendAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "events.jsonl")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Unix(42, 0) }
	for _, draft := range []Draft{
		{SessionID: "s", RunID: "r", StageID: "call", Type: "bashy.requested", CallID: "c", Data: map[string]string{"script": "pwd"}},
		{SessionID: "s", RunID: "r", StageID: "exec", Type: "bashy.completed", CallID: "c", Data: map[string]int{"exit_code": 0}},
	} {
		if _, err := store.Append(draft); err != nil {
			t.Fatal(err)
		}
	}
	events, err := Replay(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Sequence != 1 || events[1].Sequence != 2 {
		t.Fatalf("unexpected events: %#v", events)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := reopened.Append(Draft{SessionID: "s", RunID: "r2", Type: "output.emitted", Data: "done"})
	if err != nil || e.Sequence != 3 {
		t.Fatalf("append after reopen: event=%#v err=%v", e, err)
	}
}

func TestDecodeRejectsSequenceGapAndOrphanResult(t *testing.T) {
	stamp := "1970-01-01T00:00:42Z"
	for name, input := range map[string]string{
		"gap":    `{"schema_version":1,"sequence":2,"time":"` + stamp + `","session_id":"s","run_id":"r","type":"x"}`,
		"orphan": `{"schema_version":1,"sequence":1,"time":"` + stamp + `","session_id":"s","run_id":"r","type":"bashy.completed","call_id":"c"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(input)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestOpenRejectsCorruptExistingLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected corrupt log error")
	}
}

func TestCheckpointAndEventDataAreRedacted(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(Draft{SessionID: "s", RunID: "r", Type: "provider.requested", Data: map[string]any{"api_key": "secret", "nested": map[string]any{"authorization": "bearer"}}}); err != nil {
		t.Fatal(err)
	}
	cpPath := filepath.Join(dir, "checkpoints", "latest.json")
	cp, err := store.SaveCheckpoint(cpPath, "s", "r", map[string]any{"access_token": "secret", "answer": 42})
	if err != nil {
		t.Fatal(err)
	}
	if cp.Sequence != 1 {
		t.Fatalf("checkpoint sequence = %d", cp.Sequence)
	}
	for _, path := range []string{filepath.Join(dir, "events.jsonl"), cpPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "secret") || strings.Contains(string(data), "bearer") {
			t.Fatalf("credential leaked in %s: %s", path, data)
		}
	}
}
