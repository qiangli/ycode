package ycodecli

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	harnessrunner "github.com/qiangli/bashy/pkg/harnessrunner"

	harnessbashy "github.com/qiangli/ycode/internal/harness/bashy"
	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/event"
	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

func runHarnessShellOneShot(flags *shellFlags) error {
	return executeHarnessShell(context.Background(), flags, harnesscli.IO{Out: os.Stdout, Err: os.Stderr})
}

func runHarnessShellInvocation(ctx context.Context, inv harnesscli.Invocation, streams harnesscli.IO) error {
	return executeHarnessShell(ctx, &shellFlags{
		harnessFile: inv.ConfigFile, workDir: stringFlag(inv, "workdir"),
		command: stringFlag(inv, "command"), timeoutString: stringFlag(inv, "timeout"), agentRef: inv.Dispatch.AgentRef,
	}, streams)
}

func executeHarnessShell(ctx context.Context, flags *shellFlags, streams harnesscli.IO) error {
	if flags.command == "" {
		return errors.New("shell requires a command")
	}
	doc, err := loadHarness(flags.harnessFile)
	if err != nil {
		return err
	}
	agentRef := doc.Spec.Runtime.DefaultAgentRef
	if flags.agentRef != "" {
		agentRef = flags.agentRef
	}
	agent, ok := doc.Spec.Agents[agentRef]
	if !ok {
		return fmt.Errorf("shell: default agent %q is not configured", agentRef)
	}
	workspace, err := shellWorkspace(doc.BaseDir, doc.Spec.Runtime.Workspace, flags.workDir)
	if err != nil {
		return err
	}
	controlRoot := harnessspec.ControlRootPath(doc)
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
	if flags.timeoutString != "" {
		timeout, err := time.ParseDuration(flags.timeoutString)
		if err != nil || timeout <= 0 || timeout > time.Duration(bashyConfig.TimeoutMS)*time.Millisecond {
			return errors.New("execution timeout must be positive and within the compiled ceiling")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
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
	report, err := executor.Preflight(ctx, meta, call)
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
	value, err := executor.Execute(ctx, meta, call, decision.Binding)
	if err != nil {
		return err
	}
	result, ok := value.(harnessrunner.Result)
	if !ok || result.Output == nil || result.Process == nil {
		return errors.New("shell: Bashy returned an invalid result")
	}
	if err := writeChunks(streams.Out, result.Output.Stdout); err != nil {
		return err
	}
	if err := writeChunks(streams.Err, result.Output.Stderr); err != nil {
		return err
	}
	if result.Process.ExitCode != nil && *result.Process.ExitCode != 0 {
		return fmt.Errorf("shell: command exited %d", *result.Process.ExitCode)
	}
	return nil
}

func shellWorkspace(base, configured, selected string) (string, error) {
	if !filepath.IsAbs(configured) {
		configured = filepath.Join(base, configured)
	}
	root, err := filepath.EvalSymlinks(configured)
	if err != nil {
		return "", fmt.Errorf("compiled workspace: %w", err)
	}
	if selected == "" {
		return root, nil
	}
	if !filepath.IsAbs(selected) {
		selected = filepath.Join(root, selected)
	}
	cwd, err := filepath.EvalSymlinks(selected)
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	relative, err := filepath.Rel(root, cwd)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("working directory must remain within the compiled workspace")
	}
	return cwd, nil
}

func writeChunks(file io.Writer, chunks []harnessrunner.OutputChunk) error {
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
