package spec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

var stageTypes = map[string]struct{}{
	"input.read": {}, "context.load": {}, "memory.recall": {},
	"prompt.assemble": {}, "session.append": {}, "llm.call": {},
	"bashy.preflight": {}, "hitl.review": {}, "bashy.execute": {},
	"memory.write": {}, "checkpoint.save": {}, "agent.invoke": {},
	"output.emit": {},
}

var predicates = map[string]struct{}{
	"llm.finished": {}, "llm.hasToolCalls": {},
	"input.pending": {}, "output.valid": {}, "error.retryable": {},
}

// Load reads, expands, strictly decodes and validates one harness document.
func Load(path string) (*Document, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("agent config path: %w", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read agent config: %w", err)
	}
	return Compile(abs, b)
}

// Compile compiles a document whose content was already loaded.
func Compile(source string, data []byte) (*Document, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("parse agent config: %w", err)
	}
	if err := inspectNode(&node); err != nil {
		return nil, err
	}
	if err := expandNode(&node); err != nil {
		return nil, err
	}
	expanded, err := yaml.Marshal(&node)
	if err != nil {
		return nil, fmt.Errorf("encode expanded agent config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(expanded))
	dec.KnownFields(true)
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode agent config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("agent config must contain exactly one YAML document")
		}
		return nil, fmt.Errorf("decode trailing agent config: %w", err)
	}
	doc.Source = source
	doc.BaseDir = filepath.Dir(source)
	if err := doc.validate(); err != nil {
		return nil, err
	}
	if err := doc.resolveContent(); err != nil {
		return nil, err
	}
	return &doc, nil
}

func inspectNode(n *yaml.Node) error {
	if n.Alias != nil || n.Kind == yaml.AliasNode {
		return fmt.Errorf("agent config line %d: YAML aliases are not supported", n.Line)
	}
	if strings.HasPrefix(n.Tag, "!") && !strings.HasPrefix(n.Tag, "!!") {
		return fmt.Errorf("agent config line %d: custom YAML tags are not supported", n.Line)
	}
	for _, child := range n.Content {
		if err := inspectNode(child); err != nil {
			return err
		}
	}
	return nil
}

func expandNode(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		value, err := expandEnvironment(n.Value)
		if err != nil {
			return fmt.Errorf("agent config line %d: %w", n.Line, err)
		}
		n.Value = value
	}
	for _, child := range n.Content {
		if err := expandNode(child); err != nil {
			return err
		}
	}
	return nil
}

func expandEnvironment(value string) (string, error) {
	var missing string
	result := envPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := envPattern.FindStringSubmatch(match)
		if got, ok := os.LookupEnv(parts[1]); ok {
			return got
		}
		if parts[2] != "" {
			return parts[3]
		}
		missing = parts[1]
		return ""
	})
	if missing != "" {
		return "", fmt.Errorf("required environment variable %s is not set", missing)
	}
	return result, nil
}

func (d *Document) validate() error {
	if d.APIVersion != APIVersion || d.Kind != Kind {
		return fmt.Errorf("agent config: expected apiVersion %q and kind %q", APIVersion, Kind)
	}
	if d.Metadata.Name == "" || d.Metadata.Version <= 0 {
		return errors.New("agent config: metadata.name and positive metadata.version are required")
	}
	if _, ok := d.Agents[d.Harness.DefaultAgent]; !ok {
		return fmt.Errorf("agent config: default agent %q is not defined", d.Harness.DefaultAgent)
	}
	if _, ok := d.Pipelines[d.Harness.Pipeline]; !ok {
		return fmt.Errorf("agent config: harness pipeline %q is not defined", d.Harness.Pipeline)
	}
	if d.Harness.Workspace == "" || len(d.Harness.ReadableRoots) == 0 {
		return errors.New("agent config: harness.workspace and readableRoots are required")
	}
	if d.Bashy.ToolName != "bashy" {
		return errors.New(`agent config: bashy.toolName must be "bashy"`)
	}
	if d.Bashy.TimeoutMS <= 0 || d.Bashy.MaxOutputChars <= 0 || d.Bashy.MaxParallel <= 0 {
		return errors.New("agent config: positive Bashy timeout, output and parallel limits are required")
	}
	for name, model := range d.Models {
		if _, ok := d.Providers[model.Provider]; !ok {
			return fmt.Errorf("agent config: model %q references unknown provider %q", name, model.Provider)
		}
		if model.Name == "" || model.MaxOutputTokens <= 0 {
			return fmt.Errorf("agent config: model %q requires name and positive maxOutputTokens", name)
		}
	}
	for name, agent := range d.Agents {
		if _, ok := d.Models[agent.Model]; !ok {
			return fmt.Errorf("agent config: agent %q references unknown model %q", name, agent.Model)
		}
		if _, ok := d.Pipelines[agent.Pipeline]; !ok {
			return fmt.Errorf("agent config: agent %q references unknown pipeline %q", name, agent.Pipeline)
		}
		if _, ok := d.Memory[agent.Memory]; !ok {
			return fmt.Errorf("agent config: agent %q references unknown memory %q", name, agent.Memory)
		}
	}
	for name, pipeline := range d.Pipelines {
		if pipeline.Concurrency <= 0 {
			return fmt.Errorf("agent config: pipeline %q requires positive concurrency", name)
		}
		if len(pipeline.Nodes) == 0 {
			return fmt.Errorf("agent config: pipeline %q has no stages", name)
		}
		seen := make(map[string]struct{})
		if err := validateStages(pipeline.Nodes, seen); err != nil {
			return fmt.Errorf("agent config: pipeline %q: %w", name, err)
		}
		if err := validateGraph(name, pipeline); err != nil {
			return fmt.Errorf("agent config: pipeline %q: %w", name, err)
		}
		if err := validatePipelineStageContracts(name, pipeline, d.Pipelines); err != nil {
			return fmt.Errorf("agent config: pipeline %q: %w", name, err)
		}
	}
	if err := validatePipelineCalls(d.Pipelines); err != nil {
		return fmt.Errorf("agent config: %w", err)
	}
	return nil
}

func validateStages(stages []Stage, seen map[string]struct{}) error {
	for _, stage := range stages {
		forms := 0
		if stage.Use != "" {
			forms++
		}
		if stage.Call != "" {
			forms++
		}
		if stage.When != "" {
			forms++
		}
		if stage.Repeat != nil {
			forms++
		}
		if stage.Retry != nil {
			forms++
		}
		if stage.Switch != nil {
			forms++
		}
		if stage.ForEach != nil {
			forms++
		}
		if len(stage.Fallback) > 0 {
			forms++
		}
		if forms != 1 {
			return fmt.Errorf("stage %q must select exactly one control form", stage.ID)
		}
		if stage.ID != "" {
			if _, exists := seen[stage.ID]; exists {
				return fmt.Errorf("duplicate stage id %q", stage.ID)
			}
			seen[stage.ID] = struct{}{}
		}
		if stage.Use != "" {
			if _, ok := stageTypes[stage.Use]; !ok {
				return fmt.Errorf("stage %q uses unknown type %q", stage.ID, stage.Use)
			}
		}
		if stage.When != "" {
			if _, ok := predicates[stage.When]; !ok {
				return fmt.Errorf("stage %q uses unknown predicate %q", stage.ID, stage.When)
			}
			if len(stage.Stages) == 0 {
				return fmt.Errorf("conditional stage %q has no stages", stage.ID)
			}
			if err := validateStages(stage.Stages, seen); err != nil {
				return err
			}
		}
		if stage.Repeat != nil {
			if stage.Repeat.Max <= 0 || len(stage.Repeat.Stages) == 0 {
				return fmt.Errorf("repeat stage %q must be bounded and non-empty", stage.ID)
			}
			if _, ok := predicates[stage.Repeat.Until]; !ok {
				return fmt.Errorf("repeat stage %q uses unknown predicate %q", stage.ID, stage.Repeat.Until)
			}
			if err := validateStages(stage.Repeat.Stages, seen); err != nil {
				return err
			}
		}
		if stage.Retry != nil {
			if stage.Retry.Max <= 0 || len(stage.Retry.Stages) == 0 {
				return fmt.Errorf("retry stage %q must be bounded and non-empty", stage.ID)
			}
			if err := validateStages(stage.Retry.Stages, seen); err != nil {
				return err
			}
		}
		if stage.Switch != nil {
			if len(stage.Switch.Cases) == 0 {
				return fmt.Errorf("switch stage %q has no cases", stage.ID)
			}
			for _, item := range stage.Switch.Cases {
				if _, ok := predicates[item.When]; !ok || len(item.Stages) == 0 {
					return fmt.Errorf("switch stage %q has invalid case %q", stage.ID, item.When)
				}
				if err := validateStages(item.Stages, seen); err != nil {
					return err
				}
			}
			if err := validateStages(stage.Switch.Default, seen); err != nil {
				return err
			}
		}
		if stage.ForEach != nil {
			if stage.ForEach.Items == "" || stage.ForEach.As == "" || stage.ForEach.Collect == "" || stage.ForEach.MaxParallel <= 0 || len(stage.ForEach.Stages) == 0 {
				return fmt.Errorf("forEach stage %q requires items, as, collect, positive maxParallel and stages", stage.ID)
			}
			if err := validateStages(stage.ForEach.Stages, seen); err != nil {
				return err
			}
		}
		if len(stage.Fallback) > 0 {
			if err := validateStages(stage.Fallback, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePipelineCalls(pipelines map[string]Pipeline) error {
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("pipeline call cycle includes %q", name)
		}
		if visited[name] {
			return nil
		}
		visiting[name] = true
		var walk func([]Stage) error
		walk = func(stages []Stage) error {
			for _, stage := range stages {
				if stage.Call != "" {
					if _, ok := pipelines[stage.Call]; !ok {
						return fmt.Errorf("stage %q calls unknown pipeline %q", stage.ID, stage.Call)
					}
					if err := visit(stage.Call); err != nil {
						return err
					}
				}
				if err := walk(stage.Stages); err != nil {
					return err
				}
				if stage.Repeat != nil {
					if err := walk(stage.Repeat.Stages); err != nil {
						return err
					}
				}
				if stage.Retry != nil {
					if err := walk(stage.Retry.Stages); err != nil {
						return err
					}
				}
				if stage.ForEach != nil {
					if err := walk(stage.ForEach.Stages); err != nil {
						return err
					}
				}
				if stage.Switch != nil {
					for _, item := range stage.Switch.Cases {
						if err := walk(item.Stages); err != nil {
							return err
						}
					}
					if err := walk(stage.Switch.Default); err != nil {
						return err
					}
				}
				if err := walk(stage.Fallback); err != nil {
					return err
				}
			}
			return nil
		}
		if err := walk(pipelines[name].Nodes); err != nil {
			return err
		}
		visiting[name] = false
		visited[name] = true
		return nil
	}
	for name := range pipelines {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func validateGraph(name string, pipeline Pipeline) error {
	nodes := make(map[string]Stage, len(pipeline.Nodes))
	for _, node := range pipeline.Nodes {
		if node.ID == "" {
			return errors.New("top-level graph nodes require id")
		}
		nodes[node.ID] = node
	}
	for _, node := range pipeline.Nodes {
		for _, dependency := range node.Needs {
			if dependency == node.ID {
				return fmt.Errorf("node %q requires itself", node.ID)
			}
			if _, ok := nodes[dependency]; !ok {
				return fmt.Errorf("node %q requires unknown node %q", node.ID, dependency)
			}
		}
	}
	colors := make(map[string]uint8, len(nodes))
	var visit func(string) error
	visit = func(id string) error {
		if colors[id] == 1 {
			return fmt.Errorf("dependency cycle at node %q", id)
		}
		if colors[id] == 2 {
			return nil
		}
		colors[id] = 1
		for _, dependency := range nodes[id].Needs {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		colors[id] = 2
		return nil
	}
	for id := range nodes {
		if err := visit(id); err != nil {
			return err
		}
	}
	_ = name
	return nil
}

func (d *Document) resolveContent() error {
	for name, context := range d.Context {
		for i := range context.Sources {
			if err := d.resolveSource(&context.Sources[i]); err != nil {
				return fmt.Errorf("context %q: %w", name, err)
			}
		}
		d.Context[name] = context
	}
	for name, agent := range d.Agents {
		for i := range agent.System {
			if err := d.resolveSource(&agent.System[i]); err != nil {
				return fmt.Errorf("agent %q system: %w", name, err)
			}
		}
		d.Agents[name] = agent
	}
	return nil
}

func (d *Document) resolveSource(source *ContentSource) error {
	if (source.Text == "") == (source.File == "") {
		return errors.New("content source must set exactly one of text or file")
	}
	if source.File == "" {
		source.Resolved = source.Text
		return nil
	}
	path := source.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(d.BaseDir, path)
	}
	path = filepath.Clean(path)
	allowed := false
	for _, root := range d.Harness.ReadableRoots {
		if !filepath.IsAbs(root) {
			root = filepath.Join(d.BaseDir, root)
		}
		rel, err := filepath.Rel(filepath.Clean(root), path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("file %q is outside readableRoots", source.File)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !source.Required && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read file %q: %w", source.File, err)
	}
	source.Resolved = string(b)
	return nil
}
