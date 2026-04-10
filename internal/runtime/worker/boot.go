package worker

import (
	"context"
	"fmt"
	"time"
)

// BootConfig configures the worker boot process.
type BootConfig struct {
	TrustTimeout   time.Duration // max time to wait for trust resolution
	ReadyTimeout   time.Duration // max time to wait for ready state
	StartupCommand string        // command to spawn the worker process
}

// DefaultBootConfig returns sensible boot defaults.
func DefaultBootConfig() *BootConfig {
	return &BootConfig{
		TrustTimeout: 30 * time.Second,
		ReadyTimeout: 60 * time.Second,
	}
}

// Boot manages the lifecycle of a worker from spawn to ready.
type Boot struct {
	worker   *Worker
	registry *Registry
	config   *BootConfig
}

// NewBoot creates a boot manager for a worker.
func NewBoot(worker *Worker, registry *Registry, config *BootConfig) *Boot {
	if config == nil {
		config = DefaultBootConfig()
	}
	return &Boot{
		worker:   worker,
		registry: registry,
		config:   config,
	}
}

// Spawn transitions the worker from spawning to trust_required or ready_for_prompt.
func (b *Boot) Spawn(ctx context.Context) error {
	if b.worker.State != StateSpawning {
		return fmt.Errorf("worker %s not in spawning state (current: %s)", b.worker.ID, b.worker.State)
	}

	// Simulate process startup. In production, this would exec the command.
	if err := b.registry.SetState(b.worker.ID, StateTrustRequired); err != nil {
		return err
	}

	return nil
}

// ResolveTrust transitions from trust_required to ready_for_prompt.
func (b *Boot) ResolveTrust(ctx context.Context) error {
	if b.worker.State != StateTrustRequired {
		return fmt.Errorf("worker %s not in trust_required state (current: %s)", b.worker.ID, b.worker.State)
	}

	return b.registry.SetState(b.worker.ID, StateReadyForPrompt)
}

// AwaitReady waits until the worker reaches the ready_for_prompt state.
func (b *Boot) AwaitReady(ctx context.Context) error {
	deadline := time.After(b.config.ReadyTimeout)
	for {
		w, ok := b.registry.Get(b.worker.ID)
		if !ok {
			return fmt.Errorf("worker %s not found", b.worker.ID)
		}
		if w.State == StateReadyForPrompt || w.State == StateRunning {
			return nil
		}
		if w.State == StateFailed {
			return fmt.Errorf("worker %s failed: %s", b.worker.ID, w.Error)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("worker %s did not become ready within %s", b.worker.ID, b.config.ReadyTimeout)
		case <-time.After(100 * time.Millisecond):
			continue
		}
	}
}

// SendPrompt delivers a prompt to the worker and transitions to running.
func (b *Boot) SendPrompt(ctx context.Context, prompt string) error {
	w, ok := b.registry.Get(b.worker.ID)
	if !ok {
		return fmt.Errorf("worker %s not found", b.worker.ID)
	}
	if w.State != StateReadyForPrompt {
		return fmt.Errorf("worker %s not ready for prompt (current: %s)", b.worker.ID, w.State)
	}

	b.registry.mu.Lock()
	w.Prompt = prompt
	b.registry.mu.Unlock()

	return b.registry.SetState(b.worker.ID, StateRunning)
}
