package spec

import (
	"fmt"
	"sort"
	"strings"
)

var knownStageCatalog = map[string]struct{}{
	"agent.invoke": {}, "bashy.deny": {}, "bashy.execute": {}, "bashy.preflight": {},
	"bashy.reject": {}, "bashy.run": {}, "checkpoint.save": {}, "command.apply-edit": {},
	"command.finish": {}, "command.initialize": {}, "context.load": {},
	"context.measure": {}, "event.annotate": {}, "hitl.review": {},
	"input.normalize": {}, "lifecycle.transition": {}, "llm.call": {},
	"lock.acquire": {}, "lock.release": {}, "loop.finish": {}, "memory.compact": {},
	"messages.append-assistant": {},
	"messages.append-source":    {}, "messages.append-tool-results": {}, "messages.clear-tool-results": {},
	"messages.apply-input": {}, "messages.normalize-provider-response": {},
	"outcome.fail": {}, "output.emit": {}, "policy.bind-approval": {},
	"policy.evaluate": {}, "prompt.assemble": {}, "queue.drain": {},
	"state.forward": {}, "state.project": {},
}

func isKnownStage(name string) bool { _, ok := knownStageCatalog[name]; return ok }

func stagePorts(name string) (string, string) {
	switch name {
	case "event.annotate", "state.forward", "state.project":
		return "value", "value"
	case "checkpoint.save":
		return "state", "checkpoint"
	case "memory.compact":
		return "state,checkpoint", "state"
	case "loop.finish", "policy.bind-approval", "command.finish":
		return "state", "state"
	case "command.apply-edit":
		return "state,editedIntent", "state"
	case "outcome.fail":
		return "state", "state"
	case "lock.acquire":
		return "", "lease"
	case "lock.release":
		return "lease", ""
	case "bashy.execute":
		return "intent,authorization,fencingToken", "result"
	case "bashy.deny":
		return "intent,decision", "result,terminal"
	case "bashy.reject":
		return "intent,resolution", "result,terminal"
	case "hitl.review":
		return "checkpoint,intent,preflight,decision", "resolution"
	case "bashy.preflight":
		return "intent", "report"
	case "policy.evaluate":
		return "intent,preflight", "decision,authorization"
	case "command.initialize":
		return "call", "state"
	case "messages.append-tool-results":
		return "state,results", "state"
	case "messages.clear-tool-results":
		return "state", "state"
	case "queue.drain":
		return "", "items"
	case "messages.apply-input":
		return "state,items", "state"
	case "context.measure":
		return "messages", "tokens,contextBudget?,truncateBudget?,measured?"
	case "messages.append-source":
		return "state", "state"
	case "llm.call":
		return "messages,providerSession", "response,providerSession"
	case "messages.normalize-provider-response", "messages.append-assistant":
		return "state,response", "state"
	case "input.normalize":
		return "request", "input,text?"
	case "context.load":
		return "", "context"
	case "prompt.assemble":
		return "input,context,knowledge", "state"
	case "output.emit":
		return "messages", "output"
	case "lifecycle.transition":
		return "", ""
	case "agent.invoke":
		return "input", "output"
	default:
		return "", ""
	}
}

type typedSlots map[string]string

func validatePipelineContracts(pipelines map[string]Pipeline, memories map[string]Memory) error {
	if err := validateBashyRunNodes(pipelines, memories); err != nil {
		return err
	}
	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		pipeline := pipelines[name]
		if err := validateOnePipeline(name, pipeline, pipelines); err != nil {
			return fmt.Errorf("harness: pipeline %q: %w", name, err)
		}
	}
	return nil
}

func validateOnePipeline(name string, pipeline Pipeline, catalog map[string]Pipeline) error {
	if err := validateStateAndMerges(pipeline); err != nil {
		return err
	}
	nodes := make(map[string]Stage, len(pipeline.Nodes))
	for _, node := range pipeline.Nodes {
		nodes[node.ID] = node
	}
	stateTypes := make(typedSlots, len(pipeline.State))
	for slot, declaration := range pipeline.State {
		stateTypes[slot] = declaration.Type
	}
	results, visiting := make(map[string]typedSlots), make(map[string]bool)
	var compileNode func(string) (typedSlots, error)
	compileNode = func(id string) (typedSlots, error) {
		if got, ok := results[id]; ok {
			return got, nil
		}
		if visiting[id] {
			return nil, fmt.Errorf("dependency cycle at node %q", id)
		}
		visiting[id] = true
		available := cloneTyped(pipeline.Inputs)
		for _, dependency := range nodes[id].Needs {
			got, err := compileNode(dependency)
			if err != nil {
				return nil, err
			}
			mergeTyped(available, got)
		}
		node := nodes[id]
		if node.WhenExpr != nil {
			if err := validateExpression(*node.WhenExpr, available); err != nil {
				return nil, fmt.Errorf("node %q when: %w", id, err)
			}
		}
		if node.RetryV1 != nil {
			if err := validateExpression(node.RetryV1.When, outcomeSlots(available)); err != nil {
				return nil, fmt.Errorf("node %q retry.when: %w", id, err)
			}
		}
		out, err := validateRun(id, node.Run, available, stateTypes, catalog)
		if err != nil {
			return nil, err
		}
		delete(visiting, id)
		results[id] = out
		return out, nil
	}
	all := cloneTyped(pipeline.Inputs)
	for _, node := range pipeline.Nodes {
		got, err := compileNode(node.ID)
		if err != nil {
			return err
		}
		mergeTyped(all, got)
	}
	for output, want := range pipeline.Outputs {
		got, ok := all[output]
		if !ok {
			return fmt.Errorf("declared output %q is not produced", output)
		}
		if got != "" && got != want {
			return fmt.Errorf("declared output %q has type %q, produced %q", output, want, got)
		}
	}
	_ = name
	return nil
}

func validateStateAndMerges(pipeline Pipeline) error {
	for name, slot := range pipeline.State {
		if slot.Type == "" {
			return fmt.Errorf("state slot %q has no type", name)
		}
		if slot.Writer != "immutable" && slot.Writer != "single" && slot.Writer != "loop-carried" {
			return fmt.Errorf("state slot %q has invalid writer %q", name, slot.Writer)
		}
		if slot.Merge != "" && slot.Merge != "input-order" {
			return fmt.Errorf("state slot %q has invalid merge %q", name, slot.Merge)
		}
	}
	nodes := make(map[string]Stage)
	for _, node := range pipeline.Nodes {
		nodes[node.ID] = node
		if node.Run.ForEach != nil && node.Run.ForEach.MaxParallel > 1 {
			for _, target := range node.Run.ForEach.Collect {
				root := strings.SplitN(target, ".", 2)[0]
				if slot, ok := pipeline.State[root]; !ok || slot.Merge == "" {
					return fmt.Errorf("parallel forEach node %q writes %q without a deterministic state merge", node.ID, root)
				}
			}
		}
	}
	ancestor := func(candidate, node string) bool {
		seen := make(map[string]bool)
		var visit func(string) bool
		visit = func(current string) bool {
			if seen[current] {
				return false
			}
			seen[current] = true
			for _, need := range nodes[current].Needs {
				if need == candidate || visit(need) {
					return true
				}
			}
			return false
		}
		return visit(node)
	}
	for _, node := range pipeline.Nodes {
		for i, left := range node.Needs {
			for _, right := range node.Needs[i+1:] {
				if ancestor(left, right) || ancestor(right, left) {
					continue
				}
				leftWrites, rightWrites := directWrites(nodes[left]), directWrites(nodes[right])
				for slot := range leftWrites {
					if rightWrites[slot] {
						declaration, ok := pipeline.State[slot]
						if !ok || declaration.Merge == "" {
							return fmt.Errorf("node %q joins concurrent writes to %q without a deterministic merge", node.ID, slot)
						}
					}
				}
			}
		}
	}
	return nil
}

func directWrites(node Stage) map[string]bool {
	out := make(map[string]bool)
	add := func(values map[string]string) {
		for _, target := range values {
			out[strings.SplitN(target, ".", 2)[0]] = true
		}
	}
	add(node.Run.Out)
	if node.Run.Repeat != nil {
		add(node.Run.Repeat.Out)
	}
	if node.Run.ForEach != nil {
		add(node.Run.ForEach.Collect)
	}
	if node.Run.Switch != nil {
		add(node.Run.Switch.Out)
	}
	if node.Run.Fallback != nil {
		add(node.Run.Fallback.Out)
	}
	return out
}

func validateRun(id string, run Run, available, state typedSlots, catalog map[string]Pipeline) (typedSlots, error) {
	current := cloneTyped(available)
	if err := validateBindings(id, "in", run.In, available); err != nil {
		return nil, err
	}
	switch {
	case run.Stage != "":
		if !isKnownStage(run.Stage) {
			return nil, fmt.Errorf("node %q uses unknown stage %q", id, run.Stage)
		}
		// bashy.run declares free-form ports; validateBashyRunNodes owns its contract.
		if run.Stage != "bashy.run" {
			inputs, outputs := stagePorts(run.Stage)
			if err := validatePortNames(id, run.Stage, "input", run.In, inputs); err != nil {
				return nil, err
			}
			if err := validatePortNames(id, run.Stage, "output", run.Out, outputs); err != nil {
				return nil, err
			}
		}
		writeBindings(current, state, run.Out)
	case run.PipelineRef != "":
		called := catalog[run.PipelineRef]
		if err := validateCallBindings(id, run.PipelineRef, run.In, called, available); err != nil {
			return nil, err
		}
		writeCallOutputs(current, state, run.Out, called)
	case run.Repeat != nil:
		repeat := run.Repeat
		called := catalog[repeat.PipelineRef]
		if err := validateBindings(id, "repeat.carry", repeat.Carry, available); err != nil {
			return nil, err
		}
		bindings := cloneBindings(repeat.In)
		for key, value := range repeat.Carry {
			bindings[key] = value
		}
		if err := validateCallBindings(id, repeat.PipelineRef, bindings, called, available); err != nil {
			return nil, err
		}
		loopAvailable := cloneTyped(available)
		writeCallOutputs(loopAvailable, state, repeat.Out, called)
		if err := validateExpression(repeat.Until, loopAvailable); err != nil {
			return nil, fmt.Errorf("node %q repeat.until: %w", id, err)
		}
		writeCallOutputs(current, state, repeat.Out, called)
	case run.ForEach != nil:
		fan := run.ForEach
		if fan.MaxItems <= 0 {
			return nil, fmt.Errorf("node %q forEach maxItems must be positive", id)
		}
		if err := requireField(id, fan.Items.Field, available); err != nil {
			return nil, err
		}
		childAvailable := cloneTyped(available)
		childAvailable[fan.As] = ""
		if err := validateCallBindings(id, fan.PipelineRef, fan.In, catalog[fan.PipelineRef], childAvailable); err != nil {
			return nil, err
		}
		writeBindings(current, state, fan.Collect)
	case run.Switch != nil:
		sw := run.Switch
		if len(sw.Cases) == 0 {
			return nil, fmt.Errorf("node %q switch has no cases", id)
		}
		if sw.NoMatch == "" && sw.DefaultPipelineRef == "" {
			return nil, fmt.Errorf("node %q switch has no noMatch or defaultPipelineRef", id)
		}
		for _, item := range sw.Cases {
			if err := validateExpression(item.When, available); err != nil {
				return nil, fmt.Errorf("node %q switch.when: %w", id, err)
			}
			bindings := cloneBindings(sw.In)
			for key, value := range item.In {
				bindings[key] = value
			}
			if err := validateCallBindings(id, item.PipelineRef, bindings, catalog[item.PipelineRef], available); err != nil {
				return nil, err
			}
		}
		if sw.DefaultPipelineRef != "" {
			if err := validateCallBindings(id, sw.DefaultPipelineRef, sw.In, catalog[sw.DefaultPipelineRef], available); err != nil {
				return nil, err
			}
		}
		writeBindings(current, state, sw.Out)
	case run.Fallback != nil:
		fallback := run.Fallback
		for _, attempt := range fallback.Attempts {
			if len(attempt.On) == 0 {
				return nil, fmt.Errorf("node %q fallback attempt %q has no outcome classes", id, attempt.PipelineRef)
			}
			bindings := cloneBindings(fallback.In)
			for key, value := range attempt.In {
				bindings[key] = value
			}
			if err := validateCallBindings(id, attempt.PipelineRef, bindings, catalog[attempt.PipelineRef], available); err != nil {
				return nil, err
			}
		}
		if fallback.NoMatch == "" {
			return nil, fmt.Errorf("node %q fallback requires noMatch behavior", id)
		}
		writeBindings(current, state, fallback.Out)
	case run.HookInvoke != "":
		writeBindings(current, state, run.Out)
	}
	return current, nil
}

func validatePortNames(id, stage, direction string, bindings map[string]string, allowedCSV string) error {
	type portInfo struct{ required bool }
	allowed := make(map[string]portInfo)
	for _, name := range strings.Split(allowedCSV, ",") {
		if name != "" {
			required := true
			if strings.HasSuffix(name, "?") {
				name = strings.TrimSuffix(name, "?")
				required = false
			}
			allowed[name] = portInfo{required: required}
		}
	}
	for name := range bindings {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("node %q stage %q binds unknown %s port %q", id, stage, direction, name)
		}
	}
	for name, info := range allowed {
		if _, ok := bindings[name]; info.required && !ok {
			return fmt.Errorf("node %q stage %q is missing required %s port %q", id, stage, direction, name)
		}
	}
	return nil
}

func validateCallBindings(id, ref string, bindings map[string]string, called Pipeline, available typedSlots) error {
	for input, want := range called.Inputs {
		source, ok := bindings[input]
		if !ok {
			return fmt.Errorf("node %q call to %q has no binding for input %q", id, ref, input)
		}
		if err := requireField(id, source, available); err != nil {
			return err
		}
		if got, exact := available[source]; exact && got != "" && got != want {
			return fmt.Errorf("node %q binds %q (%s) to %q input %q (%s)", id, source, got, ref, input, want)
		}
	}
	for input := range bindings {
		if _, ok := called.Inputs[input]; !ok {
			return fmt.Errorf("node %q call to %q binds unknown input %q", id, ref, input)
		}
	}
	return nil
}

func validateBindings(id, label string, bindings map[string]string, available typedSlots) error {
	for _, source := range bindings {
		if err := requireField(id, source, available); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	return nil
}

func requireField(id, path string, available typedSlots) error {
	root := strings.SplitN(path, ".", 2)[0]
	if root == "" {
		return fmt.Errorf("node %q reads an empty state path", id)
	}
	if _, ok := available[root]; !ok {
		return fmt.Errorf("node %q reads %q before it is produced by a dependency", id, path)
	}
	return nil
}

func validateExpression(expr Expression, available typedSlots) error {
	type operator struct {
		name        string
		operands    []Operand
		expressions []Expression
		arity       int
		variadic    bool
	}
	ops := []operator{{"eq", expr.Eq, nil, 2, false}, {"ne", expr.Ne, nil, 2, false}, {"lt", expr.LT, nil, 2, false}, {"lte", expr.LTE, nil, 2, false}, {"gt", expr.GT, nil, 2, false}, {"gte", expr.GTE, nil, 2, false}, {"exists", expr.Exists, nil, 1, false}, {"in", expr.In, nil, 2, false}, {"all", nil, expr.All, 1, true}, {"any", nil, expr.Any, 1, true}, {"not", nil, expr.Not, 1, false}}
	selected := 0
	for _, op := range ops {
		count := len(op.operands)
		if op.expressions != nil {
			count = len(op.expressions)
		}
		if count == 0 {
			continue
		}
		selected++
		if (!op.variadic && count != op.arity) || (op.variadic && count < op.arity) {
			return fmt.Errorf("operator %s requires %s%d operand(s), got %d", op.name, map[bool]string{true: "at least ", false: ""}[op.variadic], op.arity, count)
		}
		for _, operand := range op.operands {
			if operand.Field != "" {
				if err := requireField("expression", operand.Field, available); err != nil {
					return err
				}
			} else if !operand.literalSet {
				return fmt.Errorf("operator %s has an empty operand", op.name)
			}
		}
		if op.name == "exists" && op.operands[0].Field == "" {
			return fmt.Errorf("operator exists requires a field operand")
		}
		if op.name == "in" && !isCollectionLiteral(op.operands[1]) {
			return fmt.Errorf("operator in requires a collection literal as its second operand")
		}
		if op.name == "lt" || op.name == "lte" || op.name == "gt" || op.name == "gte" {
			for _, operand := range op.operands {
				if operand.literalSet && !isOrderedLiteral(operand.Literal) {
					return fmt.Errorf("operator %s requires numeric or string literals", op.name)
				}
			}
		}
		for _, child := range op.expressions {
			if err := validateExpression(child, available); err != nil {
				return err
			}
		}
	}
	if selected != 1 {
		return fmt.Errorf("expression must select exactly one operator, got %d", selected)
	}
	return nil
}

func isCollectionLiteral(operand Operand) bool {
	if !operand.literalSet {
		return false
	}
	switch operand.Literal.(type) {
	case []any, []string, []int, []float64:
		return true
	default:
		return false
	}
}
func isOrderedLiteral(value any) bool {
	switch value.(type) {
	case string, int, int64, uint64, float32, float64:
		return true
	default:
		return false
	}
}

func writeBindings(current, state typedSlots, outputs map[string]string) {
	for _, target := range outputs {
		root := strings.SplitN(target, ".", 2)[0]
		if declared, ok := state[root]; ok {
			current[root] = declared
			continue
		}
		if _, ok := current[root]; !ok {
			current[root] = ""
		}
	}
}
func writeCallOutputs(current, state typedSlots, outputs map[string]string, called Pipeline) {
	for port, target := range outputs {
		root := strings.SplitN(target, ".", 2)[0]
		current[root] = state[root]
		if target == root {
			current[root] = called.Outputs[port]
		}
	}
}
func cloneBindings(source map[string]string) map[string]string {
	out := make(map[string]string, len(source))
	for k, v := range source {
		out[k] = v
	}
	return out
}
func cloneTyped(source map[string]string) typedSlots {
	out := make(typedSlots, len(source))
	for k, v := range source {
		out[k] = v
	}
	return out
}
func mergeTyped(dst, src typedSlots) {
	for k, v := range src {
		dst[k] = v
	}
}
func outcomeSlots(source typedSlots) typedSlots {
	out := cloneTyped(source)
	out["error"] = "node.outcome/v1"
	out["outcome"] = "node.outcome/v1"
	return out
}
