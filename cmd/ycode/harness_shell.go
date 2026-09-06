package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	harnessrunner "github.com/qiangli/bashy/pkg/harnessrunner"

	harnessbashy "github.com/qiangli/ycode/internal/harness/bashy"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

func runHarnessShellOneShot(flags *shellFlags) error {
	doc, err := loadHarness(flags.harnessFile)
	if err != nil {
		return err
	}
	agentRef := doc.Spec.Runtime.DefaultAgentRef
	agent, ok := doc.Spec.Agents[agentRef]
	if !ok {
		return fmt.Errorf("shell: default agent %q is not configured", agentRef)
	}
	workspace := flags.workDir
	if workspace == "" {
		workspace = doc.Spec.Runtime.Workspace
		if !filepath.IsAbs(workspace) {
			workspace = filepath.Join(doc.BaseDir, workspace)
		}
	}
	platform, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("shell: platform data directory: %w", err)
	}
	controlRoot := filepath.Join(platform, doc.Spec.Runtime.ControlRoot.PlatformDataDir)
	if err := os.MkdirAll(controlRoot, 0o700); err != nil {
		return fmt.Errorf("shell: control root: %w", err)
	}
	key, err := loadOrCreateAuthorizationKey(filepath.Join(controlRoot, "authorization.key"))
	if err != nil {
		return fmt.Errorf("shell: authorization key: %w", err)
	}
	events, err := event.Open(filepath.Join(controlRoot, "shell-events.jsonl"))
	if err != nil {
		return err
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(controlRoot, "payloads"))
	if err != nil {
		return err
	}
	bashyConfig := doc.Spec.Bashy.Execution
	bashyConfig.ToolName = "bashy"
	executor, err := harnessbashy.NewExecutor(bashyConfig, workspace, harnessbashy.RuntimeOptions{
		ControlRoot: filepath.Join(controlRoot, "bashy"), AuthorizationKey: key,
	}, events)
	if err != nil {
		return err
	}
	controller, err := hitl.New(hitl.Config{
		Document: doc, Events: events, Payloads: payloads,
		CheckpointPath: filepath.Join(controlRoot, "shell-hitl.json"), Preflighter: executor,
	})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	meta := hitl.Meta{
		SessionID: "shell-" + now.Format("20060102"), RunID: fmt.Sprintf("shell-%d", now.UnixNano()),
		StageID: "shell.execute", ConfigDigest: doc.ConfigDigest, Attempt: 1,
		IdempotencyKey: fmt.Sprintf("shell-%d", now.UnixNano()), PlacementID: "local",
	}
	call := hitl.Call{ID: meta.IdempotencyKey, Name: "bashy", Script: flags.command}
	report, err := executor.Preflight(context.Background(), meta, call)
	if err != nil {
		return err
	}
	decision, err := controller.Evaluate(meta, agent.PolicyRef, report)
	if err != nil {
		return err
	}
	if decision.Decision != "allow" || decision.Binding == "" {
		return fmt.Errorf("shell: policy %s decided %s; use a configured interactive frontend for review", agent.PolicyRef, decision.Decision)
	}
	value, err := executor.Execute(context.Background(), meta, call, decision.Binding)
	if err != nil {
		return err
	}
	result, ok := value.(harnessrunner.Result)
	if !ok || result.Output == nil || result.Process == nil {
		return errors.New("shell: Bashy returned an invalid result")
	}
	if err := writeChunks(os.Stdout, result.Output.Stdout); err != nil {
		return err
	}
	if err := writeChunks(os.Stderr, result.Output.Stderr); err != nil {
		return err
	}
	if result.Process.ExitCode != nil && *result.Process.ExitCode != 0 {
		return fmt.Errorf("shell: command exited %d", *result.Process.ExitCode)
	}
	return nil
}

func writeChunks(file *os.File, chunks []harnessrunner.OutputChunk) error {
	for _, chunk := range chunks {
		if chunk.Encoding != "base64" {
			return fmt.Errorf("shell: unsupported output encoding %q", chunk.Encoding)
		}
		data, err := base64.StdEncoding.DecodeString(chunk.Data)
		if err != nil {
			return err
		}
		if _, err := file.Write(data); err != nil {
			return err
		}
	}
	return nil
}

func loadOrCreateAuthorizationKey(path string) ([]byte, error) {
	if value, err := os.ReadFile(path); err == nil {
		if len(value) < 32 {
			return nil, errors.New("stored key is too short")
		}
		return value, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreateAuthorizationKey(path)
	}
	if err != nil {
		return nil, err
	}
	if _, err = file.Write(value); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	return value, nil
}
