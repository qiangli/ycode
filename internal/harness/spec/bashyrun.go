package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// BashyRunNode is the compiled contract of one harness-authored bashy.run
// graph node. Script, timeout and effects come from the node's with block;
// none has a default. The single out port binds a root state slot whose
// declared type selects how stdout is projected: type "string" receives the
// raw bytes, every other type requires stdout to parse as JSON.
type BashyRunNode struct {
	Pipeline  string
	NodeID    string
	Script    string
	TimeoutMS int
	Effects   []string
	OutPort   string
	OutTarget string
	OutType   string
}

// Input ports become YCODE_IN_<PORT> environment names, so the lowercase
// pattern keeps the uppercase mapping injective and shell-safe.
var bashyRunPortPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// BashyRunNodes compiles every bashy.run node in the document. Node ids are
// required to be unique across pipelines so that events, checkpoints and
// Bashy idempotency bindings name exactly one compiled node.
func BashyRunNodes(pipelines map[string]Pipeline) (map[string]BashyRunNode, error) {
	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string]BashyRunNode)
	for _, name := range names {
		pipeline := pipelines[name]
		for _, node := range pipeline.Nodes {
			if node.Run.Stage != "bashy.run" {
				continue
			}
			compiled, err := compileBashyRunNode(name, pipeline, node)
			if err != nil {
				return nil, err
			}
			if prior, duplicate := out[node.ID]; duplicate {
				return nil, fmt.Errorf("harness: bashy.run node id %q is declared by pipelines %q and %q; bashy.run node ids must be unique", node.ID, prior.Pipeline, name)
			}
			out[node.ID] = compiled
		}
	}
	return out, nil
}

func validateBashyRunNodes(pipelines map[string]Pipeline) error {
	_, err := BashyRunNodes(pipelines)
	return err
}

func compileBashyRunNode(pipelineName string, pipeline Pipeline, node Stage) (BashyRunNode, error) {
	where := fmt.Sprintf("harness: pipeline %q node %q", pipelineName, node.ID)
	for key := range node.Run.With {
		if key != "script" && key != "timeoutMs" && key != "effects" {
			return BashyRunNode{}, fmt.Errorf("%s: bashy.run does not accept with.%s", where, key)
		}
	}
	script, _ := node.Run.With["script"].(string)
	if strings.TrimSpace(script) == "" {
		return BashyRunNode{}, fmt.Errorf("%s: bashy.run requires with.script", where)
	}
	timeout, ok := positiveWholeInt(node.Run.With["timeoutMs"])
	if !ok {
		return BashyRunNode{}, fmt.Errorf("%s: bashy.run requires a positive integer with.timeoutMs", where)
	}
	effects := anyStringList(node.Run.With["effects"])
	if len(effects) == 0 {
		return BashyRunNode{}, fmt.Errorf("%s: bashy.run requires a non-empty with.effects ceiling", where)
	}
	if _, err := effectSet(where+" with.effects", effects); err != nil {
		return BashyRunNode{}, err
	}
	for port := range node.Run.In {
		if !bashyRunPortPattern.MatchString(port) {
			return BashyRunNode{}, fmt.Errorf("%s: bashy.run input port %q must match %s", where, port, bashyRunPortPattern)
		}
	}
	if len(node.Run.Out) != 1 {
		return BashyRunNode{}, fmt.Errorf("%s: bashy.run requires exactly one out port", where)
	}
	var outPort, outTarget string
	for port, target := range node.Run.Out {
		outPort, outTarget = port, target
	}
	if !bashyRunPortPattern.MatchString(outPort) {
		return BashyRunNode{}, fmt.Errorf("%s: bashy.run out port %q must match %s", where, outPort, bashyRunPortPattern)
	}
	if strings.Contains(outTarget, ".") {
		return BashyRunNode{}, fmt.Errorf("%s: bashy.run out port %q must target a root state slot, not %q", where, outPort, outTarget)
	}
	outType := pipeline.State[outTarget].Type
	if outType == "" {
		outType = pipeline.Outputs[outTarget]
	}
	if outType == "" {
		outType = pipeline.Inputs[outTarget]
	}
	if outType == "" {
		return BashyRunNode{}, fmt.Errorf("%s: bashy.run out target %q has no declared type in the pipeline", where, outTarget)
	}
	return BashyRunNode{
		Pipeline:  pipelineName,
		NodeID:    node.ID,
		Script:    script,
		TimeoutMS: timeout,
		Effects:   append([]string(nil), effects...),
		OutPort:   outPort,
		OutTarget: outTarget,
		OutType:   outType,
	}, nil
}

func positiveWholeInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, typed > 0
	case int64:
		return int(typed), typed > 0
	case uint64:
		return int(typed), typed > 0
	case float64:
		if typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), typed > 0
	default:
		return 0, false
	}
}

func anyStringList(value any) []string {
	if typed, ok := value.([]string); ok {
		return append([]string(nil), typed...)
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil
		}
		out = append(out, text)
	}
	return out
}
