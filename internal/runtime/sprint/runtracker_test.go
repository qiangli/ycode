package sprint

import (
	"testing"
	"time"
)

func TestRunTracker_RecordAndRetrieve(t *testing.T) {
	rt := NewRunTracker()

	rt.RecordRun(&TaskRun{
		RunID:      "run-1",
		TaskID:     "task-A",
		AgentType:  "claude",
		Status:     RunSucceeded,
		Attempt:    1,
		TokensUsed: 1000,
		Duration:   30 * time.Second,
	})
	rt.RecordRun(&TaskRun{
		RunID:      "run-2",
		TaskID:     "task-A",
		AgentType:  "claude",
		Status:     RunFailed,
		Attempt:    2,
		TokensUsed: 500,
		Duration:   10 * time.Second,
		Error:      "build failed",
	})

	runs := rt.RunsForTask("task-A")
	if len(runs) != 2 {
		t.Fatalf("runs count = %d, want 2", len(runs))
	}
	if runs[0].RunID != "run-1" {
		t.Errorf("first run = %q, want run-1", runs[0].RunID)
	}
}

func TestRunTracker_LastRun(t *testing.T) {
	rt := NewRunTracker()

	rt.RecordRun(&TaskRun{RunID: "run-1", TaskID: "t1", Attempt: 1})
	rt.RecordRun(&TaskRun{RunID: "run-2", TaskID: "t1", Attempt: 2})

	last := rt.LastRun("t1")
	if last == nil || last.RunID != "run-2" {
		t.Error("expected last run to be run-2")
	}

	if rt.LastRun("nonexistent") != nil {
		t.Error("expected nil for nonexistent task")
	}
}

func TestRunTracker_TotalTokens(t *testing.T) {
	rt := NewRunTracker()
	rt.RecordRun(&TaskRun{TaskID: "t1", TokensUsed: 1000})
	rt.RecordRun(&TaskRun{TaskID: "t1", TokensUsed: 500})
	rt.RecordRun(&TaskRun{TaskID: "t2", TokensUsed: 2000})

	if rt.TotalTokens() != 3500 {
		t.Errorf("total tokens = %d, want 3500", rt.TotalTokens())
	}
}

func TestRunTracker_SuccessRate(t *testing.T) {
	rt := NewRunTracker()
	rt.RecordRun(&TaskRun{TaskID: "t1", Status: RunSucceeded})
	rt.RecordRun(&TaskRun{TaskID: "t1", Status: RunFailed})
	rt.RecordRun(&TaskRun{TaskID: "t2", Status: RunSucceeded})
	rt.RecordRun(&TaskRun{TaskID: "t2", Status: RunSucceeded})

	rate := rt.SuccessRate()
	if rate != 0.75 {
		t.Errorf("success rate = %f, want 0.75", rate)
	}
}

func TestRunTracker_SuccessRateEmpty(t *testing.T) {
	rt := NewRunTracker()
	if rt.SuccessRate() != 0 {
		t.Errorf("empty tracker success rate = %f, want 0", rt.SuccessRate())
	}
}

func TestRunTracker_Summary(t *testing.T) {
	rt := NewRunTracker()
	rt.RecordRun(&TaskRun{TaskID: "t1", Status: RunSucceeded, TokensUsed: 100, Duration: time.Second})
	rt.RecordRun(&TaskRun{TaskID: "t1", Status: RunFailed, TokensUsed: 50, Duration: time.Second})
	rt.RecordRun(&TaskRun{TaskID: "t2", Status: RunTimedOut, TokensUsed: 200, Duration: 2 * time.Second})
	rt.RecordRun(&TaskRun{TaskID: "t3", Status: RunCancelled})

	s := rt.Summary()
	if s.TotalRuns != 4 {
		t.Errorf("total runs = %d, want 4", s.TotalRuns)
	}
	if s.Succeeded != 1 {
		t.Errorf("succeeded = %d, want 1", s.Succeeded)
	}
	if s.Failed != 1 {
		t.Errorf("failed = %d, want 1", s.Failed)
	}
	if s.TimedOut != 1 {
		t.Errorf("timed_out = %d, want 1", s.TimedOut)
	}
	if s.Cancelled != 1 {
		t.Errorf("cancelled = %d, want 1", s.Cancelled)
	}
	if s.TotalTokens != 350 {
		t.Errorf("total tokens = %d, want 350", s.TotalTokens)
	}
}

func TestRunTracker_TotalDuration(t *testing.T) {
	rt := NewRunTracker()
	rt.RecordRun(&TaskRun{TaskID: "t1", Duration: 10 * time.Second})
	rt.RecordRun(&TaskRun{TaskID: "t2", Duration: 20 * time.Second})

	if rt.TotalDuration() != 30*time.Second {
		t.Errorf("total duration = %v, want 30s", rt.TotalDuration())
	}
}
