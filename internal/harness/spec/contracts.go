package spec

import "fmt"

type stageContract struct {
	requires []string
	provides []string
}

var stageContracts = map[string]stageContract{
	"input.read":      {provides: []string{"input"}},
	"context.load":    {provides: []string{"context"}},
	"memory.recall":   {provides: []string{"memory"}},
	"prompt.assemble": {requires: []string{"input", "context"}, provides: []string{"messages"}},
	"session.append":  {},
	"llm.call":        {requires: []string{"messages"}, provides: []string{"llm.output", "llm.toolCalls", "llm.finished", "llm.hasToolCalls"}},
	"bashy.preflight": {requires: []string{"llm.toolCalls"}, provides: []string{"bashy.preflight"}},
	"hitl.review":     {requires: []string{"bashy.preflight"}, provides: []string{"bashy.approved"}},
	"bashy.execute":   {requires: []string{"bashy.approved"}, provides: []string{"bashy.results", "messages"}},
	"memory.write":    {requires: []string{"llm.output"}},
	"checkpoint.save": {},
	"agent.invoke":    {requires: []string{"input"}, provides: []string{"agent.output"}},
	"output.emit":     {requires: []string{"llm.output"}, provides: []string{"output"}},
}

var predicateSlots = map[string]string{
	"llm.finished":     "llm.finished",
	"llm.hasToolCalls": "llm.hasToolCalls",
	"input.pending":    "input.pending",
	"output.valid":     "output.valid",
	"error.retryable":  "error.retryable",
}

func validateStageContracts(stages []Stage) error {
	_, err := compileStageContracts(stages, make(map[string]struct{}))
	return err
}

func validatePipelineStageContracts(name string, pipeline Pipeline, catalog map[string]Pipeline) error {
	nodes := make(map[string]Stage, len(pipeline.Nodes))
	for _, node := range pipeline.Nodes {
		nodes[node.ID] = node
	}
	results := make(map[string]map[string]struct{}, len(nodes))
	var compileNode func(string) (map[string]struct{}, error)
	compileNode = func(id string) (map[string]struct{}, error) {
		if result, ok := results[id]; ok {
			return result, nil
		}
		available := make(map[string]struct{}, len(pipeline.Inputs))
		for _, input := range pipeline.Inputs {
			available[input] = struct{}{}
		}
		node := nodes[id]
		for _, dependency := range node.Needs {
			dependencyResult, err := compileNode(dependency)
			if err != nil {
				return nil, err
			}
			for slot := range dependencyResult {
				available[slot] = struct{}{}
			}
		}
		var result map[string]struct{}
		if node.Call != "" {
			called := catalog[node.Call]
			for _, input := range called.Inputs {
				if _, ok := available[input]; !ok {
					return nil, fmt.Errorf("node %q call to %q requires unavailable state %q", id, node.Call, input)
				}
			}
			result = cloneSlots(available)
			for _, output := range called.Outputs {
				result[output] = struct{}{}
			}
		} else {
			var err error
			result, err = compileStageContracts([]Stage{node}, available)
			if err != nil {
				return nil, err
			}
		}
		results[id] = result
		return result, nil
	}
	all := make(map[string]struct{})
	for _, node := range pipeline.Nodes {
		result, err := compileNode(node.ID)
		if err != nil {
			return err
		}
		for slot := range result {
			all[slot] = struct{}{}
		}
	}
	for _, output := range pipeline.Outputs {
		if _, ok := all[output]; !ok {
			return fmt.Errorf("declared output %q is not produced", output)
		}
	}
	_ = name
	return nil
}

func compileStageContracts(stages []Stage, available map[string]struct{}) (map[string]struct{}, error) {
	current := cloneSlots(available)
	for _, stage := range stages {
		switch {
		case stage.Use != "":
			contract := stageContracts[stage.Use]
			for _, required := range contract.requires {
				if _, ok := current[required]; !ok {
					return nil, fmt.Errorf("stage %q (%s) requires unavailable state %q", stage.ID, stage.Use, required)
				}
			}
			if stage.Use == "session.append" {
				value, ok := stage.With["value"].(string)
				if !ok || value == "" {
					return nil, fmt.Errorf("stage %q (session.append) requires with.value", stage.ID)
				}
				if _, ok := current[value]; !ok {
					return nil, fmt.Errorf("stage %q references unavailable state %q", stage.ID, value)
				}
			}
			for _, provided := range contract.provides {
				current[provided] = struct{}{}
			}
		case stage.When != "":
			if err := requirePredicate(stage.ID, stage.When, current); err != nil {
				return nil, err
			}
			if _, err := compileStageContracts(stage.Stages, current); err != nil {
				return nil, err
			}
		case stage.Repeat != nil:
			body, err := compileStageContracts(stage.Repeat.Stages, current)
			if err != nil {
				return nil, err
			}
			if err := requirePredicate(stage.ID, stage.Repeat.Until, body); err != nil {
				return nil, err
			}
			current = body
		case stage.Retry != nil:
			body, err := compileStageContracts(stage.Retry.Stages, current)
			if err != nil {
				return nil, err
			}
			current = body
		case stage.Switch != nil:
			branches := make([]map[string]struct{}, 0, len(stage.Switch.Cases)+1)
			for _, item := range stage.Switch.Cases {
				if err := requirePredicate(stage.ID, item.When, current); err != nil {
					return nil, err
				}
				branch, err := compileStageContracts(item.Stages, current)
				if err != nil {
					return nil, err
				}
				branches = append(branches, branch)
			}
			if len(stage.Switch.Default) > 0 {
				branch, err := compileStageContracts(stage.Switch.Default, current)
				if err != nil {
					return nil, err
				}
				branches = append(branches, branch)
			} else {
				branches = append(branches, current)
			}
			current = intersectSlots(branches)
		case stage.ForEach != nil:
			if _, ok := current[stage.ForEach.Items]; !ok {
				return nil, fmt.Errorf("stage %q forEach requires unavailable state %q", stage.ID, stage.ForEach.Items)
			}
			child := cloneSlots(current)
			child[stage.ForEach.As] = struct{}{}
			if _, err := compileStageContracts(stage.ForEach.Stages, child); err != nil {
				return nil, err
			}
			current[stage.ForEach.Collect] = struct{}{}
		case len(stage.Fallback) > 0:
			var branches []map[string]struct{}
			for _, alternative := range stage.Fallback {
				result, err := compileStageContracts([]Stage{alternative}, current)
				if err != nil {
					return nil, err
				}
				branches = append(branches, result)
			}
			current = intersectSlots(branches)
		}
	}
	return current, nil
}

func intersectSlots(branches []map[string]struct{}) map[string]struct{} {
	intersection := cloneSlots(branches[0])
	for _, branch := range branches[1:] {
		for slot := range intersection {
			if _, ok := branch[slot]; !ok {
				delete(intersection, slot)
			}
		}
	}
	return intersection
}

func requirePredicate(stageID, predicate string, available map[string]struct{}) error {
	slot := predicateSlots[predicate]
	if _, ok := available[slot]; !ok {
		return fmt.Errorf("stage %q predicate %q requires unavailable state %q", stageID, predicate, slot)
	}
	return nil
}

func cloneSlots(source map[string]struct{}) map[string]struct{} {
	clone := make(map[string]struct{}, len(source))
	for key := range source {
		clone[key] = struct{}{}
	}
	return clone
}
