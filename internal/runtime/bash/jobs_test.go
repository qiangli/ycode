package bash

import (
	"context"
	"syscall"
	"testing"
	"time"
)

func TestJobRegistry_StartAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	jr := NewJobRegistry()
	id, err := jr.Start(context.Background(), "echo hello", t.TempDir())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty job ID")
	}

	job, ok := jr.Get(id)
	if !ok {
		t.Fatal("job not found")
	}
	if job.Command != "echo hello" {
		t.Errorf("expected command 'echo hello', got %q", job.Command)
	}

	// Wait for completion.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job.mu.RLock()
		status := job.Status
		job.mu.RUnlock()
		if status != JobRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.Status != JobCompleted {
		t.Errorf("expected completed, got %s", job.Status)
	}
	if job.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", job.ExitCode)
	}
}

func TestJobRegistry_BackgroundOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	jr := NewJobRegistry()
	id, err := jr.Start(context.Background(), "echo line1; sleep 0.1; echo line2", t.TempDir())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for completion.
	deadline := time.Now().Add(5 * time.Second)
	job, _ := jr.Get(id)
	for time.Now().Before(deadline) {
		job.mu.RLock()
		status := job.Status
		job.mu.RUnlock()
		if status != JobRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	output := job.FullOutput()
	if output == "" {
		t.Error("expected output, got empty")
	}
}

func TestJobRegistry_Signal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	jr := NewJobRegistry()
	id, err := jr.Start(context.Background(), "sleep 60", t.TempDir())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Give process time to start.
	time.Sleep(100 * time.Millisecond)

	err = jr.SignalJob(id, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("Signal: %v", err)
	}

	// Wait for process to exit.
	deadline := time.Now().Add(5 * time.Second)
	job, _ := jr.Get(id)
	for time.Now().Before(deadline) {
		job.mu.RLock()
		status := job.Status
		job.mu.RUnlock()
		if status != JobRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.Status != JobStopped {
		t.Errorf("expected stopped, got %s", job.Status)
	}
}

func TestJobRegistry_IncrementalOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	jr := NewJobRegistry()
	id, err := jr.Start(context.Background(), "echo first; sleep 0.2; echo second", t.TempDir())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	job, _ := jr.Get(id)

	// First read — should get initial output.
	time.Sleep(100 * time.Millisecond)
	first := job.Output()
	if first == "" {
		// May not have output yet, that's ok.
		t.Log("no output on first read (timing)")
	}

	// Wait for completion and get remaining output.
	time.Sleep(400 * time.Millisecond)
	second := job.Output()
	if second == "" && first == "" {
		t.Error("expected some output across both reads")
	}
}

func TestJobRegistry_List(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	jr := NewJobRegistry()
	jr.Start(context.Background(), "true", t.TempDir())
	jr.Start(context.Background(), "true", t.TempDir())

	jobs := jr.List()
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}
