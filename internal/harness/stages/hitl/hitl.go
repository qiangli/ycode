// Package hitl implements policy evaluation and durable human-review resumes.
package hitl

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

const SchemaVersion = "ycode.hitl/v1"

type Call struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Script    string `json:"script"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type Preflight struct {
	Call        Call              `json:"call"`
	Digest      string            `json:"digest"`
	AtlasDigest string            `json:"atlas_digest,omitempty"`
	Complete    bool              `json:"complete"`
	Effects     []string          `json:"effects"`
	Paths       []string          `json:"paths"`
	EffectFacts []EffectFact      `json:"effect_facts,omitempty"`
	Unsupported []UnsupportedFact `json:"unsupported,omitempty"`
}

// EffectFact preserves Bashy's structured, pre-execution effect evidence for
// review UIs. Effects and Paths above remain the compact policy-match indexes.
type EffectFact struct {
	Kind      string `json:"kind"`
	Scope     string `json:"scope"`
	Target    string `json:"target,omitempty"`
	Source    string `json:"source"`
	Certainty string `json:"certainty"`
}

type UnsupportedFact struct {
	Kind   string `json:"kind"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

type Preflighter interface {
	Preflight(context.Context, Meta, Call) (Preflight, error)
}

type Config struct {
	Document       *spec.Document
	Events         *event.Store
	Payloads       *event.PayloadStore
	CheckpointPath string
	Preflighter    Preflighter
}

type Controller struct {
	policies       map[string]spec.Policy
	events         *event.Store
	payloads       *event.PayloadStore
	checkpointPath string
	preflighter    Preflighter

	mu      sync.Mutex
	pending *pendingReview
}

func New(config Config) (*Controller, error) {
	if config.Document == nil || config.Events == nil || config.Payloads == nil || config.CheckpointPath == "" || config.Preflighter == nil {
		return nil, errors.New("hitl requires compiled document, durable stores, checkpoint path and Bashy preflighter")
	}
	controller := &Controller{policies: clonePolicies(config.Document.Spec.Policies), events: config.Events, payloads: config.Payloads, checkpointPath: config.CheckpointPath, preflighter: config.Preflighter}
	if err := controller.restore(); err != nil {
		return nil, err
	}
	return controller, nil
}

type Meta struct {
	SessionID           string
	RunID               string
	StageID             string
	ConfigDigest        string
	StateRevision       uint64
	LifecycleGeneration uint64
	Attempt             int
	IdempotencyKey      string
	PlacementID         string
	PlacementGeneration uint64
	PolicyRevision      uint64
}

func (m Meta) validate() error {
	if m.SessionID == "" || m.RunID == "" || m.StageID == "" || m.ConfigDigest == "" {
		return errors.New("hitl stage requires session, run, stage and config digest")
	}
	return nil
}

type Decision struct {
	SchemaVersion string `json:"schema_version"`
	PolicyRef     string `json:"policy_ref"`
	RuleID        string `json:"rule_id"`
	Decision      string `json:"decision"`
	ReportDigest  string `json:"report_digest"`
	Binding       string `json:"binding,omitempty"`
}

// Evaluate applies policy rules strictly in their compiled list order.
func (c *Controller) Evaluate(meta Meta, policyRef string, report Preflight) (Decision, error) {
	if err := meta.validate(); err != nil {
		return Decision{}, err
	}
	policy, err := c.policy(policyRef)
	if err != nil {
		return Decision{}, err
	}
	if err := validateReport(report); err != nil {
		return Decision{}, err
	}
	result := Decision{SchemaVersion: SchemaVersion, PolicyRef: policyRef, ReportDigest: report.Digest}
	result.RuleID, result.Decision, err = decide(policy, report)
	if err != nil {
		return Decision{}, err
	}
	if result.Decision == "allow" {
		result.Binding = executionBinding(meta, policyRef, report.Digest)
	}
	reportRef, err := c.put(report)
	if err != nil {
		return Decision{}, err
	}
	_, err = c.append(meta, "policy.evaluated", map[string]any{"schema_version": SchemaVersion, "policy_ref": policyRef, "rule_id": result.RuleID, "decision": result.Decision, "report_digest": report.Digest, "report_ref": reportRef, "execution_binding": result.Binding})
	return result, err
}

type ReviewRequest struct {
	PolicyRef      string
	Report         Preflight
	State          any
	HumanAvailable bool
}

type Review struct {
	SchemaVersion string `json:"schema_version"`
	DecisionID    string `json:"decision_id"`
	Version       uint64 `json:"version"`
	ReviewDigest  string `json:"review_digest"`
	ReportDigest  string `json:"report_digest"`
	StateRef      string `json:"state_ref"`
	Outcome       string `json:"outcome"`
}

// Ask checkpoints pending state before emitting waiting and returning control.
func (c *Controller) Ask(meta Meta, request ReviewRequest) (Review, error) {
	if err := meta.validate(); err != nil {
		return Review{}, err
	}
	policy, err := c.policy(request.PolicyRef)
	if err != nil {
		return Review{}, err
	}
	if err := validateReport(request.Report); err != nil {
		return Review{}, err
	}
	if _, decision, err := decide(policy, request.Report); err != nil {
		return Review{}, err
	} else if decision != "ask" {
		return Review{}, fmt.Errorf("hitl ask: policy decision is %q, not ask", decision)
	}
	if !request.HumanAvailable {
		if policy.UnavailableHuman != "deny" {
			return Review{}, fmt.Errorf("hitl ask: unsupported unavailable-human behavior %q", policy.UnavailableHuman)
		}
		_, err := c.append(meta, "hitl.unavailable.denied", map[string]any{"schema_version": SchemaVersion, "policy_ref": request.PolicyRef, "report_digest": request.Report.Digest})
		return Review{SchemaVersion: SchemaVersion, ReportDigest: request.Report.Digest, Outcome: "deny"}, err
	}
	stateRef, err := c.put(request.State)
	if err != nil {
		return Review{}, err
	}
	reportRef, err := c.put(request.Report)
	if err != nil {
		return Review{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending != nil && !c.pending.Used {
		return Review{}, errors.New("hitl ask: another review is already pending")
	}
	editCount := 0
	if c.pending != nil && c.pending.Used && c.pending.LastAction == "edit" && c.pending.SessionID == meta.SessionID && c.pending.RunID == meta.RunID {
		editCount = c.pending.EditCount
	}
	if editCount > policy.MaxEdits {
		return Review{}, errors.New("hitl ask: edit count exceeds compiled policy")
	}
	pending := &pendingReview{SchemaVersion: SchemaVersion, SessionID: meta.SessionID, RunID: meta.RunID, PolicyRef: request.PolicyRef, ReportDigest: request.Report.Digest, ReportRef: reportRef, StateRef: stateRef, EditCount: editCount, Version: 1}
	pending.DecisionID = stableID(meta.SessionID, meta.RunID, request.PolicyRef, request.Report.Digest, fmt.Sprint(editCount))
	pending.ReviewDigest = pending.digest()
	if _, err = c.append(meta, "hitl.review.requested", map[string]any{"schema_version": SchemaVersion, "policy_ref": request.PolicyRef, "decision_id": pending.DecisionID, "report_digest": pending.ReportDigest, "report_ref": reportRef, "state_ref": stateRef, "edit_count": editCount}); err != nil {
		return Review{}, err
	}
	c.pending = pending
	if err := c.save(meta); err != nil {
		c.pending = nil
		return Review{}, err
	}
	if _, err = c.append(meta, "hitl.waiting", map[string]any{"schema_version": SchemaVersion, "policy_ref": request.PolicyRef, "decision_id": pending.DecisionID, "version": pending.Version, "review_digest": pending.ReviewDigest, "report_digest": pending.ReportDigest, "report_ref": reportRef, "state_ref": stateRef}); err != nil {
		return Review{}, err
	}
	return Review{SchemaVersion: SchemaVersion, DecisionID: pending.DecisionID, Version: pending.Version, ReviewDigest: pending.ReviewDigest, ReportDigest: pending.ReportDigest, StateRef: stateRef, Outcome: "waiting"}, nil
}

type ResumeRequest struct {
	DecisionID      string
	ExpectedVersion uint64
	ReviewDigest    string
	ReportDigest    string
	Action          string
	Actor           string
	EditedCall      *Call
}

type Resolution struct {
	SchemaVersion string     `json:"schema_version"`
	Action        string     `json:"action"`
	Outcome       string     `json:"outcome"`
	Binding       string     `json:"binding,omitempty"`
	EditedReport  *Preflight `json:"edited_report,omitempty"`
	EditedPolicy  *Decision  `json:"edited_policy,omitempty"`
	EditCount     int        `json:"edit_count"`
}

func (c *Controller) Resume(ctx context.Context, meta Meta, request ResumeRequest) (Resolution, error) {
	if err := meta.validate(); err != nil {
		return Resolution{}, err
	}
	if request.Actor == "" {
		return Resolution{}, errors.New("hitl resume: authenticated actor is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := c.pending
	if pending == nil || pending.Used {
		return Resolution{}, errors.New("hitl resume: decision is duplicate or no longer pending")
	}
	if pending.SessionID != meta.SessionID || pending.RunID != meta.RunID {
		return Resolution{}, errors.New("hitl resume: pending review belongs to another run")
	}
	if request.DecisionID != pending.DecisionID || request.ExpectedVersion != pending.Version || request.ReviewDigest != pending.ReviewDigest || request.ReportDigest != pending.ReportDigest {
		return Resolution{}, errors.New("hitl resume: stale decision digest or CAS version")
	}
	policy, err := c.policy(pending.PolicyRef)
	if err != nil {
		return Resolution{}, err
	}
	if !contains(policy.Resume, request.Action) {
		return Resolution{}, fmt.Errorf("hitl resume: action %q is not allowed", request.Action)
	}
	if request.Action == "edit" && (request.EditedCall == nil || pending.EditCount >= policy.MaxEdits) {
		return Resolution{}, errors.New("hitl resume: edit missing or edit limit reached")
	}
	if request.Action != "edit" && request.EditedCall != nil {
		return Resolution{}, errors.New("hitl resume: edited call supplied for non-edit action")
	}
	if _, err = c.append(meta, "hitl.decision.requested", map[string]any{"schema_version": SchemaVersion, "decision_id": pending.DecisionID, "version": pending.Version, "review_digest": pending.ReviewDigest, "report_digest": pending.ReportDigest, "action": request.Action, "actor": request.Actor}); err != nil {
		return Resolution{}, err
	}
	pending.Used = true
	pending.Version++
	pending.LastAction = request.Action
	if request.Action == "edit" {
		pending.EditCount++
	}
	oldReviewDigest := pending.ReviewDigest
	pending.ReviewDigest = pending.digest()
	if err := c.save(meta); err != nil {
		pending.Used = false
		pending.Version--
		pending.LastAction = ""
		if request.Action == "edit" {
			pending.EditCount--
		}
		pending.ReviewDigest = oldReviewDigest
		return Resolution{}, err
	}

	resolution := Resolution{SchemaVersion: SchemaVersion, Action: request.Action, Outcome: request.Action, EditCount: pending.EditCount}
	switch request.Action {
	case "approve":
		resolution.Binding = executionBinding(meta, pending.PolicyRef, pending.ReportDigest)
	case "reject":
	case "edit":
		report, preflightErr := c.preflighter.Preflight(ctx, meta, *request.EditedCall)
		if preflightErr != nil {
			_, _ = c.append(meta, "hitl.edit.preflight.failed", map[string]any{"schema_version": SchemaVersion, "decision_id": pending.DecisionID, "error": preflightErr.Error()})
			return resolution, preflightErr
		}
		if !reflect.DeepEqual(report.Call, *request.EditedCall) {
			return resolution, errors.New("hitl edit: preflight report is not bound to edited call")
		}
		decision, evaluateErr := c.evaluateWithoutLock(meta, pending.PolicyRef, report)
		if evaluateErr != nil {
			return resolution, evaluateErr
		}
		resolution.EditedReport, resolution.EditedPolicy, resolution.Outcome = &report, &decision, "repreflighted"
	}
	_, err = c.append(meta, "hitl.resolved", map[string]any{"schema_version": SchemaVersion, "decision_id": pending.DecisionID, "version": pending.Version, "action": resolution.Action, "outcome": resolution.Outcome, "execution_binding": resolution.Binding, "edited_report_digest": digestOf(resolution.EditedReport)})
	return resolution, err
}

func (c *Controller) evaluateWithoutLock(meta Meta, policyRef string, report Preflight) (Decision, error) {
	return c.Evaluate(meta, policyRef, report)
}

func decide(policy spec.Policy, report Preflight) (string, string, error) {
	if !report.Complete {
		if policy.IncompletePreflight != "deny" {
			return "", "", fmt.Errorf("policy evaluate: unsupported incomplete-preflight behavior %q", policy.IncompletePreflight)
		}
		return "incomplete-preflight", "deny", nil
	}
	for _, rule := range policy.Rules {
		if !match(rule.Match, report) {
			continue
		}
		if rule.Decision != "allow" && rule.Decision != "deny" && rule.Decision != "ask" {
			return "", "", fmt.Errorf("policy evaluate: invalid decision %q", rule.Decision)
		}
		return rule.ID, rule.Decision, nil
	}
	return "", "", errors.New("policy evaluate: no rule matched; refusing implicit default")
}
