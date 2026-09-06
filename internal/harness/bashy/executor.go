// Package bashy adapts the one model-visible tool to Bashy's policy-neutral
// harness runner. Policy is evaluated by ycode; Bashy compiles effects,
// verifies the resulting authorization binding, and performs the operation.
package bashy

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	harnessrunner "github.com/qiangli/bashy/pkg/harnessrunner"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

// Meta is the trusted execution identity carried by the graph interpreter.
// Zero revisions/generations are preserved; the adapter never fabricates
// authority that the graph has not supplied.
type Meta = hitl.Meta

type Call = hitl.Call

type RuntimeOptions struct {
	// ControlRoot is private durable state for authorization-bound jobs and
	// binary spools. It must not be within the model-writable working tree.
	ControlRoot      string
	AuthorizationKey []byte
	StdinBytes       int64
	PlacementID      string
}

type Executor struct {
	config     spec.Bashy
	dir        string
	store      *event.Store
	manager    *harnessrunner.Manager
	stdinBytes int64
	placement  string
}

func NewExecutor(config spec.Bashy, dir string, options RuntimeOptions, store *event.Store) (*Executor, error) {
	if config.ToolName != "bashy" || config.TimeoutMS <= 0 || config.MaxOutputChars <= 0 || config.MaxSpillBytes <= 0 {
		return nil, errors.New("bashy executor requires the compiled Bashy configuration")
	}
	if config.Environment.Inherit != "allowlist" {
		return nil, errors.New("bashy executor requires environment.inherit=allowlist")
	}
	if options.ControlRoot == "" || len(options.AuthorizationKey) < 32 {
		return nil, errors.New("bashy executor requires a trusted control root and 32-byte authorization key")
	}
	if options.StdinBytes < 0 {
		return nil, errors.New("bashy executor stdin limit cannot be negative")
	}
	abs, err := canonicalDirectory(dir)
	if err != nil {
		return nil, fmt.Errorf("bashy cwd: %w", err)
	}
	controlAbs, err := canonicalProspectivePath(options.ControlRoot)
	if err != nil {
		return nil, fmt.Errorf("bashy control root: %w", err)
	}
	if within(abs, controlAbs) {
		return nil, errors.New("bashy control root must be outside the model-writable cwd")
	}
	manager, err := harnessrunner.New(controlAbs, options.AuthorizationKey)
	if err != nil {
		return nil, fmt.Errorf("bashy control root: %w", err)
	}
	placement := options.PlacementID
	if placement == "" {
		placement = "local"
	}
	return &Executor{config: config, dir: abs, store: store, manager: manager, stdinBytes: options.StdinBytes, placement: placement}, nil
}

func canonicalProspectivePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

func canonicalDirectory(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return filepath.Clean(abs), nil
}

func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Preflight returns both the compact policy indexes and the complete typed
// Bashy evidence used by a human review UI.
func (e *Executor) Preflight(_ context.Context, meta Meta, call hitl.Call) (hitl.Preflight, error) {
	req, err := e.request(meta, call, harnessrunner.OperationPreflight)
	if err != nil {
		return hitl.Preflight{}, err
	}
	if err := e.requested(meta, req); err != nil {
		return hitl.Preflight{}, err
	}
	result := e.manager.Preflight(req)
	report := reportFor(call, result)
	if err := e.finished(meta, req, result); err != nil {
		return hitl.Preflight{}, err
	}
	if result.Intent == nil {
		return report, protocolError(result)
	}
	return report, nil
}

// Execute recomputes the intent and creates a digest-bound authorization from
// the graph's allow decision. Non-zero process exits remain typed results.
func (e *Executor) Execute(ctx context.Context, meta Meta, call hitl.Call, grantDigest string) (any, error) {
	return e.run(ctx, meta, call, grantDigest, harnessrunner.OperationExecute)
}

// Start is the durable counterpart of Execute. The returned process binding
// and generation are required for every later control operation.
func (e *Executor) Start(ctx context.Context, meta Meta, call hitl.Call, grantDigest string) (harnessrunner.Result, error) {
	return e.run(ctx, meta, call, grantDigest, harnessrunner.OperationStart)
}

func (e *Executor) run(ctx context.Context, meta Meta, call hitl.Call, grantDigest string, operation harnessrunner.Operation) (harnessrunner.Result, error) {
	if err := validDigest(grantDigest); err != nil {
		return harnessrunner.Result{}, fmt.Errorf("bashy %s authorization: %w", operation, err)
	}
	req, err := e.request(meta, call, operation)
	if err != nil {
		return harnessrunner.Result{}, err
	}
	preflight := e.manager.Preflight(req)
	if preflight.Intent == nil || !preflight.Intent.Complete {
		if preflight.Error != nil {
			return preflight, protocolError(preflight)
		}
		return preflight, errors.New("bashy execution requires complete preflight evidence")
	}
	claims := harnessrunner.AuthorizationClaims{
		Binding: req.Binding, IntentDigest: preflight.Intent.Digest,
		PolicyRevision: meta.PolicyRevision, GrantDigest: grantDigest,
	}
	if e.config.JobLifetimeMS > 0 {
		claims.ExpiresAt = time.Now().UTC().Add(time.Duration(e.config.JobLifetimeMS) * time.Millisecond)
	}
	authorization, err := e.manager.Authorize(*preflight.Intent, claims)
	if err != nil {
		return harnessrunner.Result{}, fmt.Errorf("bashy authorize: %w", err)
	}
	req.IntentDigest, req.AuthorizationBinding = preflight.Intent.Digest, &authorization
	if err := e.requested(meta, req); err != nil {
		return harnessrunner.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return harnessrunner.Result{}, err
	}
	var result harnessrunner.Result
	switch operation {
	case harnessrunner.OperationExecute:
		result = e.manager.Execute(ctx, req)
	case harnessrunner.OperationStart:
		result = e.manager.Start(req)
	default:
		return harnessrunner.Result{}, fmt.Errorf("bashy: unsupported launch operation %q", operation)
	}
	if err := e.finished(meta, req, result); err != nil {
		return harnessrunner.Result{}, err
	}
	if result.Protocol == "error" {
		return result, protocolError(result)
	}
	return result, nil
}

type Control struct {
	Operation     harnessrunner.Operation
	RequestID     string
	Binding       harnessrunner.Binding
	JobID         string
	JobGeneration uint64
	StdoutCursor  int64
	StderrCursor  int64
	MaxChunkBytes int64
	InputSequence uint64
	Input         []byte
	Signal        harnessrunner.Signal
}

// Control performs one of poll/stdin/signal/cancel using the same graph
// binding as Start. Binary data is transported only as base64 plus a digest.
func (e *Executor) Control(ctx context.Context, meta Meta, control Control) (harnessrunner.Result, error) {
	switch control.Operation {
	case harnessrunner.OperationPoll, harnessrunner.OperationStdin, harnessrunner.OperationSignal, harnessrunner.OperationCancel:
	default:
		return harnessrunner.Result{}, fmt.Errorf("bashy: unsupported control operation %q", control.Operation)
	}
	if control.RequestID == "" || control.JobID == "" || control.JobGeneration == 0 {
		return harnessrunner.Result{}, errors.New("bashy control requires request, job and job generation")
	}
	if err := validateControlBinding(meta, control.Binding); err != nil {
		return harnessrunner.Result{}, err
	}
	req := harnessrunner.Request{
		SchemaVersion: harnessrunner.RequestSchemaVersion, RequestID: control.RequestID,
		Operation: control.Operation, Binding: control.Binding, JobID: control.JobID,
		JobGeneration: control.JobGeneration, StdoutCursor: control.StdoutCursor,
		StderrCursor: control.StderrCursor, MaxChunkBytes: control.MaxChunkBytes,
	}
	if control.Operation == harnessrunner.OperationStdin {
		digest := digestBytes(control.Input)
		req.Input = &harnessrunner.InputChunk{Sequence: control.InputSequence, Encoding: "base64", Data: base64.StdEncoding.EncodeToString(control.Input), Digest: digest}
	}
	req.Signal = control.Signal
	if err := e.requested(meta, req); err != nil {
		return harnessrunner.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return harnessrunner.Result{}, err
	}
	result := e.manager.Handle(ctx, req)
	if err := e.finished(meta, req, result); err != nil {
		return harnessrunner.Result{}, err
	}
	if result.Protocol == "error" {
		return result, protocolError(result)
	}
	return result, nil
}

func validateControlBinding(meta Meta, binding harnessrunner.Binding) error {
	if meta.SessionID == "" || meta.RunID == "" || meta.StageID == "" || meta.ConfigDigest == "" {
		return errors.New("bashy requires session, run, stage and config digest")
	}
	// NodeID and Attempt identify the originating start node. A routed poll or
	// cancel stage naturally has a different current StageID, so Bashy's job
	// lookup verifies those origin fields while ycode verifies the shared run,
	// state, config and lifecycle authority here.
	if binding.RunID != meta.RunID || binding.NodeID == "" || binding.Attempt <= 0 ||
		binding.StateRevision > meta.StateRevision || binding.ConfigDigest != meta.ConfigDigest ||
		binding.LifecycleGeneration != meta.LifecycleGeneration || binding.IdempotencyKey == "" {
		return errors.New("bashy control binding does not match the current graph authority")
	}
	return nil
}

func (e *Executor) request(meta Meta, call hitl.Call, operation harnessrunner.Operation) (harnessrunner.Request, error) {
	if call.ID == "" || call.Name != e.config.ToolName || call.Script == "" {
		return harnessrunner.Request{}, errors.New("bashy call requires id, name=bashy and script")
	}
	binding, err := bindingFor(meta, call.ID)
	if err != nil {
		return harnessrunner.Request{}, err
	}
	timeoutMS := e.config.TimeoutMS
	if call.TimeoutMS > 0 && call.TimeoutMS < timeoutMS {
		timeoutMS = call.TimeoutMS
	}
	maxBytes := e.config.MaxSpillBytes
	if maxBytes < e.config.MaxOutputChars {
		maxBytes = e.config.MaxOutputChars
	}
	return harnessrunner.Request{
		SchemaVersion: harnessrunner.RequestSchemaVersion,
		RequestID:     stableID(string(operation), meta.SessionID, meta.RunID, call.ID, fmt.Sprint(meta.LifecycleGeneration)),
		Operation:     operation, Binding: binding,
		Command:   harnessrunner.Command{Script: call.Script, Cwd: e.dir, Environment: e.environment()},
		Limits:    harnessrunner.Limits{WallTimeMs: int64(timeoutMS), StdoutBytes: int64(maxBytes), StderrBytes: int64(maxBytes), StdinBytes: e.stdinBytes},
		Placement: harnessrunner.Placement{ID: placementID(meta, e.placement), Generation: meta.PlacementGeneration},
	}, nil
}

func bindingFor(meta Meta, fallbackIdempotency string) (harnessrunner.Binding, error) {
	if meta.SessionID == "" || meta.RunID == "" || meta.StageID == "" || meta.ConfigDigest == "" {
		return harnessrunner.Binding{}, errors.New("bashy requires session, run, stage and config digest")
	}
	attempt := meta.Attempt
	if attempt == 0 {
		attempt = 1
	}
	idempotency := meta.IdempotencyKey
	if idempotency == "" {
		idempotency = stableID("bashy-idempotency-v1", meta.SessionID, meta.RunID, fallbackIdempotency, fmt.Sprint(meta.LifecycleGeneration))
	}
	return harnessrunner.Binding{
		RunID: meta.RunID, NodeID: meta.StageID, Attempt: attempt,
		StateRevision: meta.StateRevision, ConfigDigest: meta.ConfigDigest,
		LifecycleGeneration: meta.LifecycleGeneration, IdempotencyKey: idempotency,
	}, nil
}

func placementID(meta Meta, fallback string) string {
	if meta.PlacementID != "" {
		return meta.PlacementID
	}
	return fallback
}

func (e *Executor) environment() []harnessrunner.EnvironmentVariable {
	names := append([]string(nil), e.config.Environment.Names...)
	sort.Strings(names)
	result := make([]harnessrunner.EnvironmentVariable, 0, len(names)+1)
	seen := make(map[string]bool, len(names)+1)
	for _, name := range names {
		if name == "" || seen[name] || name == "BASHY_AGENTIC" {
			continue
		}
		seen[name] = true
		if value, ok := os.LookupEnv(name); ok {
			result = append(result, harnessrunner.EnvironmentVariable{Name: name, ValueRef: "environment://" + name, Value: value})
		}
	}
	result = append(result, harnessrunner.EnvironmentVariable{Name: "BASHY_AGENTIC", ValueRef: "harness://agentic", Value: "1"})
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func reportFor(call hitl.Call, result harnessrunner.Result) hitl.Preflight {
	report := hitl.Preflight{Call: call}
	if result.Intent == nil {
		return report
	}
	report.Digest = result.Intent.Digest
	report.AtlasDigest = result.Intent.AtlasDigest
	report.Complete = result.Intent.Complete
	effects, paths := make(map[string]bool), make(map[string]bool)
	for _, fact := range result.Intent.Effects {
		report.EffectFacts = append(report.EffectFacts, hitl.EffectFact{Kind: fact.Kind, Scope: fact.Scope, Target: fact.Target, Source: fact.Source, Certainty: fact.Certainty})
		effects[fact.Kind] = true
		if fact.Scope != "" {
			paths[fact.Scope] = true
		}
	}
	for _, fact := range result.Intent.Unsupported {
		report.Unsupported = append(report.Unsupported, hitl.UnsupportedFact{Kind: fact.Kind, Value: fact.Value, Reason: fact.Reason})
	}
	report.Effects = sortedKeys(effects)
	report.Paths = sortedKeys(paths)
	return report
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (e *Executor) requested(meta Meta, req harnessrunner.Request) error {
	return e.append(meta, "bashy.requested", req.RequestID, map[string]any{
		"schema_version": harnessrunner.RequestSchemaVersion, "operation": req.Operation,
		"request_id": req.RequestID, "binding": req.Binding, "intent_digest": req.IntentDigest,
		"job_id": req.JobID, "job_generation": req.JobGeneration,
		"stdout_cursor": req.StdoutCursor, "stderr_cursor": req.StderrCursor,
	})
}

func (e *Executor) finished(meta Meta, req harnessrunner.Request, result harnessrunner.Result) error {
	kind := "bashy.completed"
	if result.Protocol == "error" || result.Outcome == harnessrunner.OutcomeFailed || result.Outcome == harnessrunner.OutcomeBlocked || result.Outcome == harnessrunner.OutcomeAmbiguous {
		kind = "bashy.failed"
	}
	return e.append(meta, kind, req.RequestID, result)
}

func (e *Executor) append(meta Meta, kind, callID string, data any) error {
	if e.store == nil {
		return nil
	}
	_, err := e.store.Append(event.Draft{
		SessionID: meta.SessionID, RunID: meta.RunID, StageID: meta.StageID,
		Type: kind, CallID: callID, ConfigDigest: meta.ConfigDigest,
		StateBefore: meta.StateRevision, StateAfter: meta.StateRevision, Data: data,
	})
	return err
}

func protocolError(result harnessrunner.Result) error {
	if result.Error == nil {
		return errors.New("bashy runner returned an invalid protocol result")
	}
	return fmt.Errorf("bashy %s: %s: %s", result.Operation, result.Error.Code, result.Error.Message)
}

func validDigest(value string) error {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return errors.New("grant digest must be a sha256 digest")
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stableID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
