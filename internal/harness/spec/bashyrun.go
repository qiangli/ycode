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

// A script placeholder either names an input port or the reserved run
// binding {{session}} (both substituted as single-quoted literals at stage
// time, before preflight), or one of the {{memory.*}} policy values resolved
// here at compile time from with.memoryRef. Literal substitution is the only
// supported input path into a command argument: the Bashy intent analyzer
// treats any shell variable expansion in an argument as unprovable, so a
// $YCODE_IN_<PORT> consumer is always denied under the real boundary.
var bashyRunPlaceholderPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// BashyRunNodes compiles every bashy.run node in the document. Node ids are
// required to be unique across pipelines so that events, checkpoints and
// Bashy idempotency bindings name exactly one compiled node. Memory-policy
// placeholders are resolved against the declared memories.
func BashyRunNodes(pipelines map[string]Pipeline, memories map[string]Memory) (map[string]BashyRunNode, error) {
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
			compiled, err := compileBashyRunNode(name, pipeline, node, memories)
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

func validateBashyRunNodes(pipelines map[string]Pipeline, memories map[string]Memory) error {
	_, err := BashyRunNodes(pipelines, memories)
	return err
}

func compileBashyRunNode(pipelineName string, pipeline Pipeline, node Stage, memories map[string]Memory) (BashyRunNode, error) {
	where := fmt.Sprintf("harness: pipeline %q node %q", pipelineName, node.ID)
	for key := range node.Run.With {
		if key != "script" && key != "timeoutMs" && key != "effects" && key != "memoryRef" {
			return BashyRunNode{}, fmt.Errorf("%s: bashy.run does not accept with.%s", where, key)
		}
	}
	script, _ := node.Run.With["script"].(string)
	if strings.TrimSpace(script) == "" {
		return BashyRunNode{}, fmt.Errorf("%s: bashy.run requires with.script", where)
	}
	script, err := resolveBashyRunPlaceholders(where, script, node.Run, memories)
	if err != nil {
		return BashyRunNode{}, err
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
		if port == "session" {
			return BashyRunNode{}, fmt.Errorf("%s: bashy.run input port %q is reserved for the run session binding", where, port)
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

// resolveBashyRunPlaceholders substitutes {{memory.*}} policy values now and
// proves every remaining placeholder is resolvable at stage time. The result
// still carries {{port}} and {{session}} markers; the turn runtime replaces
// those with single-quoted literals before preflight, so the digest-bound
// authorization always covers the fully composed script.
func resolveBashyRunPlaceholders(where, script string, run Run, memories map[string]Memory) (string, error) {
	memoryRef, hasMemoryRef := run.With["memoryRef"].(string)
	if raw, declared := run.With["memoryRef"]; declared && (!hasMemoryRef || memoryRef == "") {
		return "", fmt.Errorf("%s: bashy.run with.memoryRef must be a memory name, got %v", where, raw)
	}
	memory, memoryDeclared := memories[memoryRef]
	if hasMemoryRef && !memoryDeclared {
		return "", fmt.Errorf("%s: bashy.run with.memoryRef references unknown memory %q", where, memoryRef)
	}
	var resolveErr error
	resolved := bashyRunPlaceholderPattern.ReplaceAllStringFunc(script, func(match string) string {
		if resolveErr != nil {
			return match
		}
		name := strings.TrimSpace(match[2 : len(match)-2])
		if value, ok := strings.CutPrefix(name, "memory."); ok {
			if !hasMemoryRef {
				resolveErr = fmt.Errorf("%s: script placeholder {{%s}} requires with.memoryRef", where, name)
				return match
			}
			switch value {
			case "rings":
				return strings.Join(memory.Recall.Rings, ",")
			case "forms":
				return strings.Join(memory.Recall.Forms, ",")
			case "budget":
				return fmt.Sprintf("%d", memory.Recall.MaxTokens)
			case "k":
				return fmt.Sprintf("%d", memory.Recall.MaxItems)
			default:
				resolveErr = fmt.Errorf("%s: script placeholder {{%s}} is not a compiled memory policy value (rings, forms, budget, k)", where, name)
				return match
			}
		}
		if name == "session" {
			return match
		}
		if _, ok := run.In[name]; !ok {
			resolveErr = fmt.Errorf("%s: script placeholder {{%s}} does not name an input port", where, name)
		}
		return match
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return resolved, nil
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
