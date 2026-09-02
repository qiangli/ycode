package bashy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func TestExecutorRunsOnlyBashyAndRecordsPair(t *testing.T) {
	dir := t.TempDir()
	store, err := event.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewExecutor(spec.Bashy{ToolName: "bashy", TimeoutMS: 1000, MaxOutputChars: 1024}, dir, store)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), "session", "run", "execute", Call{ID: "call-1", Name: "bashy", Script: "printf harness"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Stdout != "harness" {
		t.Fatalf("result = %#v", result)
	}
	events, err := event.Replay(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != "bashy.requested" || events[1].Type != "bashy.completed" {
		t.Fatalf("events = %#v", events)
	}
}

func TestExecutorRejectsAnyOtherTool(t *testing.T) {
	executor, err := NewExecutor(spec.Bashy{ToolName: "bashy", TimeoutMS: 1000, MaxOutputChars: 1024}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), "s", "r", "x", Call{ID: "c", Name: "read_file", Script: "true"}); err == nil {
		t.Fatal("non-Bashy tool was accepted")
	}
}
