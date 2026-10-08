package event

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIncrementalReaderDecodesOnlyAppendedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var log bytes.Buffer
	previous := ""
	for sequence := uint64(1); sequence <= 50_000; sequence++ {
		data := json.RawMessage(`{}`)
		e := Event{SchemaVersion: SchemaVersion, Sequence: sequence, Time: time.Unix(42, 0).UTC(), SessionID: "old", RunID: "run", Type: "synthetic", Data: data, PayloadDigest: digestBytes(data), PreviousDigest: previous}
		var err error
		e.Digest, err = eventDigest(e)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		log.Write(raw)
		log.WriteByte('\n')
		previous = e.Digest
	}
	if err := os.WriteFile(path, log.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := NewReader(path)
	if events, err := reader.Replay(); err != nil || len(events) != 50_000 {
		t.Fatalf("initial replay = %d events, %v", len(events), err)
	}
	if _, err := reader.Replay(); err != nil {
		t.Fatal(err)
	}
	if got := reader.tail.BytesRead(); got != 0 {
		t.Fatalf("unchanged log decoded %d old bytes", got)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	appended, err := store.Append(Draft{SessionID: "new", RunID: "run", Type: "session.control-requested", Data: map[string]string{"action": "pause"}})
	if err != nil {
		t.Fatal(err)
	}
	if events, err := reader.Replay(); err != nil || len(events) != 50_001 || events[len(events)-1].Sequence != appended.Sequence {
		t.Fatalf("tail replay = %d events, last=%d, err=%v", len(events), events[len(events)-1].Sequence, err)
	}
	if got := reader.tail.BytesRead(); got <= 0 || got >= int64(log.Len()) {
		t.Fatalf("decoded %d bytes; expected only the appended tail, not %d-byte history", got, log.Len())
	}
}

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

func TestReplayRejectsTamperedPayloadAndChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(Draft{SessionID: "s", RunID: "r", Type: "one", Data: "original"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "original", "tampered", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Replay(path); err == nil || !strings.Contains(err.Error(), "payload digest mismatch") {
		t.Fatalf("replay error = %v", err)
	}
}

func TestPayloadStoreRoundTripAndIntegrity(t *testing.T) {
	store, err := OpenPayloadStore(filepath.Join(t.TempDir(), "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := store.Put([]byte("model-visible bytes"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(digest)
	if err != nil || string(got) != "model-visible bytes" {
		t.Fatalf("get = %q, %v", got, err)
	}
	if again, err := store.Put(got); err != nil || again != digest {
		t.Fatalf("idempotent put = %q, %v", again, err)
	}
	if err := os.WriteFile(store.path(digest), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(digest); err == nil {
		t.Fatal("expected corruption error")
	}
}

// Sprint: #302; Story: #1039; Story-ID: 37bbc5482cde
//
// Two processes (here: two Stores) append to ONE event log — concurrent genie
// runs share ycode's harness sessions log. Each Store used to cache the next
// sequence and the chain tip, so interleaved appends wrote duplicate
// sequences and every later Open failed ("event line N: sequence N-1").
func TestTwoStoresShareOneLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, s := range []*Store{a, b, a, b} {
		wg.Add(1)
		go func(s *Store) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				if _, err := s.Append(Draft{SessionID: "s", RunID: "r", Type: "t"}); err != nil {
					t.Error(err)
					return
				}
			}
		}(s)
	}
	wg.Wait()
	events, err := Replay(path)
	if err != nil {
		t.Fatalf("log corrupted by interleaved appends: %v", err)
	}
	if len(events) != 100 {
		t.Fatalf("got %d events, want 100", len(events))
	}
	if _, err := Open(path); err != nil {
		t.Fatal(err)
	}
}
