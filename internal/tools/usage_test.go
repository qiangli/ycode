package tools

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withTempUsageDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := usageDir
	usageDir = func() (string, bool) { return dir, true }
	t.Cleanup(func() { usageDir = prev })
	return dir
}

func TestRecordSkillUsage_AppendsValidJSONLine(t *testing.T) {
	dir := withTempUsageDir(t)

	recordSkillUsage("commit", 12, usageSourceExternalBuiltin, nil, 35*time.Millisecond)

	path := filepath.Join(dir, "skill-usage.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read usage log: %v", err)
	}
	var ev usageEvent
	if err := json.Unmarshal(stripTrailingNewline(data), &ev); err != nil {
		t.Fatalf("usage line is not valid JSON: %v\nraw: %s", err, data)
	}
	if ev.Name != "commit" {
		t.Errorf("Name = %q, want %q", ev.Name, "commit")
	}
	if ev.Source != usageSourceExternalBuiltin {
		t.Errorf("Source = %q, want %q", ev.Source, usageSourceExternalBuiltin)
	}
	if ev.ArgsLen != 12 {
		t.Errorf("ArgsLen = %d, want 12", ev.ArgsLen)
	}
	if !ev.Ok {
		t.Error("Ok = false, want true")
	}
	if ev.ErrKind != "" {
		t.Errorf("ErrKind = %q, want empty", ev.ErrKind)
	}
	if ev.LatencyMs != 35 {
		t.Errorf("LatencyMs = %d, want 35", ev.LatencyMs)
	}
	if _, err := time.Parse(time.RFC3339, ev.Ts); err != nil {
		t.Errorf("Ts %q is not RFC3339: %v", ev.Ts, err)
	}
}

func TestRecordSkillUsage_MultipleEventsAppend(t *testing.T) {
	dir := withTempUsageDir(t)

	recordSkillUsage("commit", 0, usageSourceExternalBuiltin, nil, time.Millisecond)
	recordSkillUsage("nope", 4, usageSourceNotFound, errors.New("skill \"nope\" not found"), time.Millisecond)
	recordSkillUsage("review", 8, usageSourceExternal, nil, 5*time.Millisecond)

	path := filepath.Join(dir, "skill-usage.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open usage log: %v", err)
	}
	defer f.Close()

	var lines []usageEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev usageEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("invalid JSON line: %v\nraw: %s", err, scanner.Text())
		}
		lines = append(lines, ev)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d events, want 3", len(lines))
	}
	wantNames := []string{"commit", "nope", "review"}
	for i, want := range wantNames {
		if lines[i].Name != want {
			t.Errorf("event %d name = %q, want %q", i, lines[i].Name, want)
		}
	}
	if lines[1].Ok {
		t.Error("not_found event Ok = true, want false")
	}
	if lines[1].ErrKind != "not_found" {
		t.Errorf("not_found event ErrKind = %q, want %q", lines[1].ErrKind, "not_found")
	}
}

func TestRecordSkillUsage_FailQuietWhenHomeUnavailable(t *testing.T) {
	prev := usageDir
	usageDir = func() (string, bool) { return "", false }
	t.Cleanup(func() { usageDir = prev })

	// Should not panic, should not error, should be a no-op.
	recordSkillUsage("anything", 0, usageSourceExternal, nil, 0)
}

func TestClassifyErr(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("skill \"x\" not found"), "not_found"},
		{errors.New("skill \"x\" uses executor=cnl which is not yet supported"), "cnl_unsupported"},
		{errors.New("something went sideways"), "executor_error"},
	}
	for _, tc := range cases {
		got := classifyErr(tc.err)
		if got != tc.want {
			t.Errorf("classifyErr(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func stripTrailingNewline(b []byte) []byte {
	return []byte(strings.TrimRight(string(b), "\n"))
}
