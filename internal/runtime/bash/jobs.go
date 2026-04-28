package bash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// JobStatus represents the lifecycle state of a background job.
type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobStopped   JobStatus = "stopped"
)

// Job represents a background command execution.
type Job struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	Status    JobStatus `json:"status"`
	PID       int       `json:"pid"`
	ExitCode  int       `json:"exit_code"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`

	stdout *RingBuffer
	stderr *RingBuffer
	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// Output returns the incremental output (stdout + stderr) since the last call.
func (j *Job) Output() string {
	j.mu.RLock()
	defer j.mu.RUnlock()

	out := j.stdout.Incremental()
	errOut := j.stderr.Incremental()
	if errOut != "" {
		if out != "" {
			out += "\n"
		}
		out += errOut
	}
	return out
}

// FullOutput returns all buffered output regardless of read position.
func (j *Job) FullOutput() string {
	j.mu.RLock()
	defer j.mu.RUnlock()

	out := j.stdout.String()
	errOut := j.stderr.String()
	if errOut != "" {
		if out != "" {
			out += "\n"
		}
		out += errOut
	}
	return out
}

// Signal sends a signal to the background process group.
// Callers should check job status before calling this; this method only
// validates that the process handle exists.
func (j *Job) Signal(sig os.Signal) error {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if j.cmd == nil || j.cmd.Process == nil {
		return fmt.Errorf("process not running")
	}

	// Send to process group (negative PID) so child processes also receive it.
	sysSig, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("unsupported signal type")
	}
	return syscall.Kill(-j.cmd.Process.Pid, sysSig)
}

// StatusSummary returns a human-readable status string.
func (j *Job) StatusSummary() string {
	j.mu.RLock()
	defer j.mu.RUnlock()

	switch j.Status {
	case JobRunning:
		elapsed := time.Since(j.StartedAt).Truncate(time.Second)
		return fmt.Sprintf("running (%s elapsed)", elapsed)
	case JobCompleted:
		return fmt.Sprintf("completed (exit code %d, ran %s)", j.ExitCode, j.EndedAt.Sub(j.StartedAt).Truncate(time.Second))
	case JobFailed:
		return fmt.Sprintf("failed (exit code %d)", j.ExitCode)
	case JobStopped:
		return "stopped by signal"
	default:
		return string(j.Status)
	}
}

// JobRegistry manages background job lifecycle. Thread-safe.
type JobRegistry struct {
	mu     sync.RWMutex
	jobs   map[string]*Job
	nextID int
}

// NewJobRegistry creates a new job registry.
func NewJobRegistry() *JobRegistry {
	return &JobRegistry{
		jobs: make(map[string]*Job),
	}
}

// Start launches a command in the background, returning its job ID immediately.
func (jr *JobRegistry) Start(ctx context.Context, command string, workDir string) (string, error) {
	jr.mu.Lock()
	jr.nextID++
	id := fmt.Sprintf("job_%d", jr.nextID)
	jr.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout := NewRingBuffer(MaxOutputSize)
	stderr := NewRingBuffer(MaxOutputSize)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin, _ = os.Open(os.DevNull)

	if err := cmd.Start(); err != nil {
		cancel()
		return "", fmt.Errorf("start background job: %w", err)
	}

	job := &Job{
		ID:        id,
		Command:   command,
		Status:    JobRunning,
		PID:       cmd.Process.Pid,
		StartedAt: time.Now(),
		stdout:    stdout,
		stderr:    stderr,
		cmd:       cmd,
		cancel:    cancel,
	}

	jr.mu.Lock()
	jr.jobs[id] = job
	jr.mu.Unlock()

	// Cleanup goroutine: wait for process to exit and update status.
	go func() {
		err := cmd.Wait()
		job.mu.Lock()
		job.EndedAt = time.Now()
		if job.Status == JobStopped {
			// Signal was sent — keep "stopped" status.
			job.mu.Unlock()
			return
		}
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				job.ExitCode = exitErr.ExitCode()
			}
			job.Status = JobFailed
		} else {
			job.ExitCode = 0
			job.Status = JobCompleted
		}
		job.mu.Unlock()
	}()

	return id, nil
}

// Get retrieves a job by ID.
func (jr *JobRegistry) Get(id string) (*Job, bool) {
	jr.mu.RLock()
	defer jr.mu.RUnlock()
	job, ok := jr.jobs[id]
	return job, ok
}

// List returns all jobs.
func (jr *JobRegistry) List() []*Job {
	jr.mu.RLock()
	defer jr.mu.RUnlock()
	result := make([]*Job, 0, len(jr.jobs))
	for _, j := range jr.jobs {
		result = append(result, j)
	}
	return result
}

// SignalJob sends a signal to a job by ID.
func (jr *JobRegistry) SignalJob(id string, sig os.Signal) error {
	job, ok := jr.Get(id)
	if !ok {
		return fmt.Errorf("unknown job: %s", id)
	}
	// Mark as stopped before sending signal.
	job.mu.Lock()
	if job.Status != JobRunning {
		job.mu.Unlock()
		return fmt.Errorf("job %s is not running (status: %s)", id, job.Status)
	}
	if sig == syscall.SIGTERM || sig == syscall.SIGKILL {
		job.Status = JobStopped
		job.EndedAt = time.Now()
	}
	job.mu.Unlock()

	return job.Signal(sig)
}
