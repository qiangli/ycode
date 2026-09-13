package turn

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

// compileBashyRunIndex resolves every bashy.run node once at construction so
// the stage handler executes only compiled, ceiling-checked contracts. A node
// effect outside spec.bashy.execution.effectsCeiling fails construction.
func compileBashyRunIndex(doc *spec.Document) (map[string]spec.BashyRunNode, error) {
	nodes, err := spec.BashyRunNodes(doc.Spec.Pipelines)
	if err != nil {
		return nil, err
	}
	ceiling := make(map[string]struct{}, len(doc.Spec.Bashy.Execution.EffectsCeiling))
	for _, effect := range doc.Spec.Bashy.Execution.EffectsCeiling {
		ceiling[effect] = struct{}{}
	}
	for id, node := range nodes {
		for _, effect := range node.Effects {
			if _, ok := ceiling[effect]; !ok {
				return nil, fmt.Errorf("turn: bashy.run node %q effect %q exceeds spec.bashy.execution.effectsCeiling", id, effect)
			}
		}
	}
	return nodes, nil
}

// bashyRun executes a harness-authored Bashy call as a neutral graph stage.
// The call follows the exact model-tool path: preflight, policy evaluation,
// digest-bound authorization, execution. The node's declared effects are a
// ceiling enforced against preflight evidence, a non-zero exit fails closed,
// and stdout is projected into the single declared out port.
func (r *Runtime) bashyRun(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	node, ok := r.bashyRuns[in.StageID]
	if !ok {
		return fail(fmt.Errorf("bashy.run: node %q is not in the compiled document", in.StageID))
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	run, err := runFrom(ctx)
	if err != nil {
		return fail(err)
	}
	script, err := composeBashyRunScript(node.Script, in.Inputs)
	if err != nil {
		return fail(fmt.Errorf("bashy.run: encode typed inputs: %w", err))
	}
	call := hitl.Call{ID: stableID("bashy.run", run.sessionID, run.runID, in.StageID, script), Name: "bashy", Script: script, TimeoutMS: node.TimeoutMS}
	requestRef, err := r.payload(map[string]any{"call": call, "effects": node.Effects, "out_port": node.OutPort})
	if err != nil {
		return fail(err)
	}
	if err := r.append(ctx, in.StageID, "bashy.run.requested", map[string]any{"call_id": call.ID, "payload_ref": requestRef, "effects": node.Effects}); err != nil {
		return fail(err)
	}
	report, err := r.bashy.Preflight(ctx, r.bashyMeta(ctx, meta, call.ID), call)
	if err != nil {
		return r.bashyRunFinish(ctx, in.StageID, call.ID, "failed", map[string]any{"error": err.Error()}, err)
	}
	if !report.Complete {
		err := errors.New("bashy.run: incomplete preflight evidence is denied")
		return r.bashyRunFinish(ctx, in.StageID, call.ID, "denied", map[string]any{"report": report}, err)
	}
	if effect := firstEffectOutside(report.Effects, node.Effects); effect != "" {
		err := fmt.Errorf("bashy.run: preflight effect %q exceeds the node ceiling %v", effect, node.Effects)
		return r.bashyRunFinish(ctx, in.StageID, call.ID, "denied", map[string]any{"report": report}, err)
	}
	policyRef := r.doc.Spec.Agents[run.agentRef].PolicyRef
	decision, err := r.hitl.Evaluate(hitlMeta(meta), policyRef, report)
	if err != nil {
		return r.bashyRunFinish(ctx, in.StageID, call.ID, "failed", map[string]any{"error": err.Error()}, err)
	}
	if decision.Decision != "allow" || decision.Binding == "" {
		err := fmt.Errorf("bashy.run: policy %q decision %q; a harness-authored call runs only on allow", policyRef, decision.Decision)
		return r.bashyRunFinish(ctx, in.StageID, call.ID, "denied", map[string]any{"decision": decision}, err)
	}
	result, err := r.bashy.Execute(ctx, r.bashyMeta(ctx, meta, call.ID), call, decision.Binding)
	if err != nil {
		return r.bashyRunFinish(ctx, in.StageID, call.ID, "failed", map[string]any{"error": err.Error(), "result": result}, err)
	}
	evidence, err := decodeBashyRunResult(result)
	if err != nil {
		return r.bashyRunFinish(ctx, in.StageID, call.ID, "failed", map[string]any{"error": err.Error(), "result": result}, err)
	}
	if evidence.outcome != "completed" || evidence.exitCode == nil || *evidence.exitCode != 0 {
		err := fmt.Errorf("bashy.run: command failed closed (outcome %q, exit %s): %s", evidence.outcome, exitText(evidence.exitCode), strings.TrimSpace(string(evidence.stderr)))
		return r.bashyRunFinish(ctx, in.StageID, call.ID, "failed", map[string]any{"result": result}, err)
	}
	value, err := parseBashyRunOutput(node.OutType, evidence.stdout)
	if err != nil {
		err = fmt.Errorf("bashy.run: stdout does not satisfy out port type %q: %w", node.OutType, err)
		return r.bashyRunFinish(ctx, in.StageID, call.ID, "failed", map[string]any{"error": err.Error(), "result": result}, err)
	}
	responseRef, err := r.payload(result)
	if err != nil {
		return fail(err)
	}
	if err := r.append(ctx, in.StageID, "bashy.run.completed", map[string]any{"call_id": call.ID, "payload_ref": responseRef, "outcome": "completed"}); err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{node.OutPort: value})
}

// bashyRunFinish records the terminal event with its payload reference and
// fails the node; denial and failure never produce a port value.
func (r *Runtime) bashyRunFinish(ctx context.Context, stageID, callID, outcome string, evidence map[string]any, cause error) pipeline.Outcome {
	ref, err := r.payload(evidence)
	if err != nil {
		return fail(err)
	}
	if err := r.append(ctx, stageID, "bashy.run.completed", map[string]any{"call_id": callID, "payload_ref": ref, "outcome": outcome}); err != nil {
		return fail(err)
	}
	return pipeline.Failure("bashy.run-"+outcome, false, cause)
}

// composeBashyRunScript binds typed inputs to the authored script: every port
// is a member of one stdin JSON object, and scalar ports are additionally
// exported as YCODE_IN_<PORT>. The composed script is what preflight analyzes
// and what the digest-bound authorization covers.
func composeBashyRunScript(script string, inputs map[string]any) (string, error) {
	if inputs == nil {
		inputs = map[string]any{}
	}
	payload, err := json.Marshal(inputs)
	if err != nil {
		return "", err
	}
	ports := make([]string, 0, len(inputs))
	for port := range inputs {
		ports = append(ports, port)
	}
	sort.Strings(ports)
	var composed strings.Builder
	for _, port := range ports {
		value, scalar := scalarEnvValue(inputs[port])
		if !scalar {
			continue
		}
		composed.WriteString("export YCODE_IN_" + strings.ToUpper(port) + "=" + shellSingleQuote(value) + "\n")
	}
	composed.WriteString("printf '%s' " + shellSingleQuote(string(payload)) + " | {\n")
	composed.WriteString(script)
	composed.WriteString("\n}")
	return composed.String(), nil
}

func scalarEnvValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		return strconv.FormatBool(typed), true
	case int:
		return strconv.Itoa(typed), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case uint64:
		return strconv.FormatUint(typed, 10), true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case json.Number:
		return typed.String(), true
	default:
		return "", false
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func firstEffectOutside(observed, ceiling []string) string {
	allowed := make(map[string]struct{}, len(ceiling))
	for _, effect := range ceiling {
		allowed[effect] = struct{}{}
	}
	for _, effect := range observed {
		if _, ok := allowed[effect]; !ok {
			return effect
		}
	}
	return ""
}

type bashyRunEvidence struct {
	outcome  string
	exitCode *int
	stdout   []byte
	stderr   []byte
}

// decodeBashyRunResult reads the typed execution evidence through a JSON
// round-trip so the handler depends on the wire contract, not on the concrete
// boundary implementation.
func decodeBashyRunResult(result any) (bashyRunEvidence, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return bashyRunEvidence{}, err
	}
	var wire struct {
		Outcome string `json:"outcome"`
		Process *struct {
			ExitCode *int `json:"exitCode"`
		} `json:"process"`
		Output *struct {
			Stdout []struct {
				Encoding string `json:"encoding"`
				Data     string `json:"data"`
			} `json:"stdout"`
			Stderr []struct {
				Encoding string `json:"encoding"`
				Data     string `json:"data"`
			} `json:"stderr"`
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return bashyRunEvidence{}, err
	}
	evidence := bashyRunEvidence{outcome: wire.Outcome}
	if wire.Process != nil {
		evidence.exitCode = wire.Process.ExitCode
	}
	if wire.Output != nil {
		for _, chunk := range wire.Output.Stdout {
			data, err := decodeBashyRunChunk(chunk.Encoding, chunk.Data)
			if err != nil {
				return bashyRunEvidence{}, err
			}
			evidence.stdout = append(evidence.stdout, data...)
		}
		for _, chunk := range wire.Output.Stderr {
			data, err := decodeBashyRunChunk(chunk.Encoding, chunk.Data)
			if err != nil {
				return bashyRunEvidence{}, err
			}
			evidence.stderr = append(evidence.stderr, data...)
		}
	}
	return evidence, nil
}

func decodeBashyRunChunk(encoding, data string) ([]byte, error) {
	if encoding != "base64" {
		return nil, fmt.Errorf("bashy.run: unsupported output encoding %q", encoding)
	}
	return base64.StdEncoding.DecodeString(data)
}

// parseBashyRunOutput projects stdout into the declared out port type: the
// "string" type receives the raw bytes, every other declared type requires a
// single JSON document.
func parseBashyRunOutput(portType string, stdout []byte) (any, error) {
	if portType == "string" {
		return string(stdout), nil
	}
	var value any
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func exitText(code *int) string {
	if code == nil {
		return "none"
	}
	return strconv.Itoa(*code)
}
