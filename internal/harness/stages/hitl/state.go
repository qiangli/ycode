package hitl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

type pendingReview struct {
	SchemaVersion string `json:"schema_version"`
	SessionID     string `json:"session_id"`
	RunID         string `json:"run_id"`
	PolicyRef     string `json:"policy_ref"`
	DecisionID    string `json:"decision_id"`
	Version       uint64 `json:"version"`
	ReviewDigest  string `json:"review_digest"`
	ReportDigest  string `json:"report_digest"`
	ReportRef     string `json:"report_ref"`
	StateRef      string `json:"state_ref"`
	EditCount     int    `json:"edit_count"`
	LastAction    string `json:"last_action,omitempty"`
	Used          bool   `json:"used"`
}

func (p pendingReview) digest() string {
	p.ReviewDigest = ""
	data, _ := json.Marshal(p)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type checkpointState struct {
	SchemaVersion string         `json:"schema_version"`
	Pending       *pendingReview `json:"pending,omitempty"`
}

func (c *Controller) save(meta Meta) error {
	_, err := c.events.SaveCheckpoint(c.checkpointPath, meta.SessionID, meta.RunID, checkpointState{SchemaVersion: SchemaVersion, Pending: c.pending})
	return err
}

func (c *Controller) restore() error {
	data, err := os.ReadFile(c.checkpointPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("hitl restore checkpoint: %w", err)
	}
	var checkpoint event.Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return fmt.Errorf("hitl restore checkpoint: %w", err)
	}
	if checkpoint.SchemaVersion != event.SchemaVersion {
		return errors.New("hitl restore checkpoint: unsupported event schema")
	}
	var state checkpointState
	if err := json.Unmarshal(checkpoint.State, &state); err != nil {
		return fmt.Errorf("hitl restore state: %w", err)
	}
	if state.SchemaVersion != SchemaVersion {
		return errors.New("hitl restore state: unsupported schema")
	}
	if state.Pending != nil {
		if state.Pending.SessionID != checkpoint.SessionID || state.Pending.RunID != checkpoint.RunID || state.Pending.ReviewDigest != state.Pending.digest() {
			return errors.New("hitl restore state: checkpoint binding mismatch")
		}
		copy := *state.Pending
		c.pending = &copy
	}
	return nil
}

func (c *Controller) put(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return c.payloads.Put(data)
}

func (c *Controller) append(meta Meta, kind string, data any) (event.Event, error) {
	return c.events.Append(event.Draft{SessionID: meta.SessionID, RunID: meta.RunID, StageID: meta.StageID, Type: kind, ConfigDigest: meta.ConfigDigest, Data: data})
}

func (c *Controller) policy(ref string) (spec.Policy, error) {
	policy, ok := c.policies[ref]
	if !ok || policy.ApprovalBinding != "execution-context-digest-v1" || policy.MaxEdits < 0 || len(policy.Resume) == 0 {
		return spec.Policy{}, fmt.Errorf("hitl: invalid compiled policy %q", ref)
	}
	return policy, nil
}

func validateReport(report Preflight) error {
	if report.Call.ID == "" || report.Call.Name != "bashy" || report.Call.Script == "" || report.Digest == "" {
		return errors.New("policy evaluate: invalid Bashy preflight report")
	}
	if hasDuplicate(report.Effects) || hasDuplicate(report.Paths) {
		return errors.New("policy evaluate: duplicate preflight effects or paths")
	}
	return nil
}

func match(rule spec.PolicyMatch, report Preflight) bool {
	if rule.Always {
		return true
	}
	matchedConstraint := false
	if rule.PreflightComplete != nil {
		matchedConstraint = true
		if report.Complete != *rule.PreflightComplete {
			return false
		}
	}
	if len(rule.EffectsAny) > 0 {
		matchedConstraint = true
		if !intersects(report.Effects, rule.EffectsAny) {
			return false
		}
	}
	if len(rule.EffectsAllWithin) > 0 {
		matchedConstraint = true
		if !allWithin(report.Effects, rule.EffectsAllWithin) {
			return false
		}
	}
	if len(rule.PathsAllWithin) > 0 {
		matchedConstraint = true
		if !allWithin(report.Paths, rule.PathsAllWithin) {
			return false
		}
	}
	return matchedConstraint
}

func allWithin(values, allowed []string) bool {
	for _, value := range values {
		if !contains(allowed, value) {
			return false
		}
	}
	return true
}

func intersects(left, right []string) bool {
	for _, value := range left {
		if contains(right, value) {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasDuplicate(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return true
		}
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func executionBinding(meta Meta, policyRef, reportDigest string) string {
	return stableID("execution-context-digest-v1", meta.SessionID, meta.RunID, meta.ConfigDigest, policyRef, reportDigest)
}

func stableID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func digestOf(value any) string {
	if value == nil || reflect.ValueOf(value).IsNil() {
		return ""
	}
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func clonePolicies(in map[string]spec.Policy) map[string]spec.Policy {
	out := make(map[string]spec.Policy, len(in))
	for key, value := range in {
		value.Resume = append([]string(nil), value.Resume...)
		value.Rules = append([]spec.PolicyRule(nil), value.Rules...)
		for index := range value.Rules {
			value.Rules[index].Match.EffectsAny = append([]string(nil), value.Rules[index].Match.EffectsAny...)
			value.Rules[index].Match.EffectsAllWithin = append([]string(nil), value.Rules[index].Match.EffectsAllWithin...)
			value.Rules[index].Match.PathsAllWithin = append([]string(nil), value.Rules[index].Match.PathsAllWithin...)
			if value.Rules[index].Match.PreflightComplete != nil {
				copy := *value.Rules[index].Match.PreflightComplete
				value.Rules[index].Match.PreflightComplete = &copy
			}
		}
		out[key] = value
	}
	return out
}
