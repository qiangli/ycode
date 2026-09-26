package spec

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var resourceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-.][a-z0-9]+)*$`)

func Load(path string) (*Document, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("harness path: %w", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read harness: %w", err)
	}
	return Compile(abs, data)
}

func Compile(source string, data []byte) (*Document, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, located(source, fmt.Errorf("parse harness: %w", err))
	}
	if err := inspectNode(&node); err != nil {
		return nil, located(source, err)
	}
	if err := applyYAMLDefaults(&node); err != nil {
		return nil, located(source, err)
	}
	compiledYAML, err := yaml.Marshal(&node)
	if err != nil {
		return nil, located(source, fmt.Errorf("compile defaults: %w", err))
	}
	dec := yaml.NewDecoder(bytes.NewReader(compiledYAML))
	dec.KnownFields(true)
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return nil, located(source, fmt.Errorf("decode harness: %w", err))
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, located(source, errors.New("harness must contain exactly one YAML document"))
		}
		return nil, located(source, fmt.Errorf("decode trailing harness document: %w", err))
	}
	if err := validateCLINode(&node); err != nil {
		return nil, located(source, err)
	}
	doc.Source, doc.BaseDir = source, filepath.Dir(source)
	if err := doc.resolveImports(); err != nil {
		return nil, located(source, err)
	}
	doc.Agents, doc.Pipelines = doc.Spec.Agents, doc.Spec.Pipelines
	if err := doc.validate(); err != nil {
		return nil, located(source, err)
	}
	if err := doc.resolveSources(); err != nil {
		return nil, located(source, err)
	}
	doc.resolveValues()
	doc.ConfigDigest = doc.graphDigest()
	return &doc, nil
}

func (d *Document) resolveValues() {
	resolve := func(value *Value) {
		if got, ok := os.LookupEnv(value.ValueFrom.Env); ok {
			value.Resolved = got
		} else {
			value.Resolved = value.ValueFrom.Default
		}
	}
	for name, provider := range d.Spec.Providers {
		resolve(&provider.Endpoint)
		d.Spec.Providers[name] = provider
	}
	resolve(&d.Spec.Observability.Endpoint)
}

func located(source string, err error) error { return fmt.Errorf("%s: %w", source, err) }

func inspectNode(n *yaml.Node) error {
	if n.Alias != nil || n.Kind == yaml.AliasNode {
		return fmt.Errorf("harness line %d: YAML aliases are not supported", n.Line)
	}
	if strings.HasPrefix(n.Tag, "!") && !strings.HasPrefix(n.Tag, "!!") {
		return fmt.Errorf("harness line %d: custom YAML tags are not supported", n.Line)
	}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" && strings.Contains(n.Value, "${") {
		return fmt.Errorf("harness line %d: scalar environment interpolation is not supported; use valueFrom or secretRef", n.Line)
	}
	for _, child := range n.Content {
		if err := inspectNode(child); err != nil {
			return err
		}
	}
	return nil
}

func (d *Document) validate() error {
	if d.APIVersion != APIVersion || d.Kind != Kind {
		return fmt.Errorf("harness: expected apiVersion %q and kind %q", APIVersion, Kind)
	}
	if d.Metadata.Name == "" || d.Metadata.Version <= 0 {
		return errors.New("harness: metadata.name and positive metadata.version are required")
	}
	if d.Spec.Runtime.Workspace == "" || len(d.Spec.Runtime.ReadableRoots) == 0 {
		return errors.New("harness: spec.runtime.workspace and readableRoots are required")
	}
	if err := validateResourceNames(reflect.ValueOf(d.Spec)); err != nil {
		return err
	}
	if err := validateStructuredValues(d); err != nil {
		return err
	}
	if err := validateTypedReferences(d); err != nil {
		return err
	}
	if err := ValidateCLI(d); err != nil {
		return err
	}
	if err := validateReachability(d); err != nil {
		return err
	}
	if err := validateControlRoot(d); err != nil {
		return err
	}
	if err := validateAuthority(d); err != nil {
		return err
	}
	if err := validatePlatformCompatibility(d); err != nil {
		return err
	}
	if err := validateMemories(d.Spec.Memories); err != nil {
		return err
	}
	if err := validatePipelines(d.Spec.Pipelines); err != nil {
		return err
	}
	if err := validatePipelineContracts(d.Spec.Pipelines, d.Spec.Memories); err != nil {
		return err
	}
	return validateBashy(d.Spec.Bashy)
}

// validateResourceNames applies to named resource maps only. Lower camel case
// is accepted because frontend IDs are protocol identifiers in the design
// fixture; separators themselves remain lowercase and unambiguous.
func validateResourceNames(specValue reflect.Value) error {
	t := specValue.Type()
	for i := 0; i < specValue.NumField(); i++ {
		field, value := t.Field(i), specValue.Field(i)
		if value.Kind() != reflect.Map || field.Name == "Extensions" {
			continue
		}
		for _, key := range value.MapKeys() {
			if key.Kind() != reflect.String {
				continue
			}
			name := key.String()
			if !resourceNamePattern.MatchString(name) {
				return fmt.Errorf("harness: spec.%s has invalid resource name %q", field.Tag.Get("yaml"), name)
			}
		}
	}
	return nil
}

func validateStructuredValues(d *Document) error {
	checkValue := func(path string, value Value) error {
		if value.ValueFrom.Env == "" {
			return fmt.Errorf("harness: %s.valueFrom.env is required", path)
		}
		return nil
	}
	for name, provider := range d.Spec.Providers {
		if err := checkValue("spec.providers."+name+".endpoint", provider.Endpoint); err != nil {
			return err
		}
		if err := validateSecret("spec.providers."+name+".credentials.apiKey", provider.Credentials.APIKey.SecretRef); err != nil {
			return err
		}
		for key, header := range provider.Headers {
			if err := validateSecret("spec.providers."+name+".headers."+key, header.SecretRef); err != nil {
				return err
			}
		}
	}
	if err := checkValue("spec.observability.endpoint", d.Spec.Observability.Endpoint); err != nil {
		return err
	}
	for name, frontend := range d.Spec.Frontends {
		if frontend.Auth != nil {
			if err := validateSecret("spec.frontends."+name+".auth", frontend.Auth.SecretRef); err != nil {
				return err
			}
		}
		if err := validateFrontendUI(name, frontend); err != nil {
			return err
		}
	}
	return nil
}

// validateFrontendUI admits the one built-in page: ui: chat on a bearer-auth
// http frontend.
func validateFrontendUI(name string, frontend Frontend) error {
	if frontend.UI == "" {
		return nil
	}
	if frontend.UI != "chat" || frontend.Kind != "http" {
		return fmt.Errorf("harness: spec.frontends.%s.ui: only ui: chat on an http frontend is supported", name)
	}
	if frontend.Auth == nil || frontend.Auth.Mode != "bearer" {
		return fmt.Errorf("harness: spec.frontends.%s.ui requires bearer auth", name)
	}
	return nil
}

func validateSecret(path string, ref SecretRef) error {
	if ref.Provider != "env" || ref.Name == "" {
		return fmt.Errorf("harness: %s.secretRef requires provider=env and a name", path)
	}
	return nil
}

type referenceTarget struct {
	path  string
	names map[string]struct{}
}

func validateTypedReferences(d *Document) error {
	targets := make(map[string]referenceTarget)
	register := func(field, path string, values any) {
		v := reflect.ValueOf(values)
		names := make(map[string]struct{}, v.Len())
		for _, key := range v.MapKeys() {
			names[key.String()] = struct{}{}
		}
		targets[field] = referenceTarget{path: path, names: names}
	}
	register("defaultAgentRef", "spec.agents", d.Spec.Agents)
	register("agentRef", "spec.agents", d.Spec.Agents)
	register("providerRef", "spec.providers", d.Spec.Providers)
	register("modelRef", "spec.models", d.Spec.Models)
	register("modelRouteRef", "spec.routes", d.Spec.Routes)
	register("routeRef", "spec.routes", d.Spec.Routes)
	register("pipelineRef", "spec.pipelines", d.Spec.Pipelines)
	register("defaultPipelineRef", "spec.pipelines", d.Spec.Pipelines)
	register("sourceRef", "spec.sources", d.Spec.Sources)
	register("instructionRef", "spec.sources", d.Spec.Sources)
	register("contextRef", "spec.contexts", d.Spec.Contexts)
	register("memoryRef", "spec.memories", d.Spec.Memories)
	register("queueRef", "spec.queues", d.Spec.Queues)
	register("sessionRef", "spec.sessions", d.Spec.Sessions)
	register("lifecycleRef", "spec.lifecycles", d.Spec.Lifecycles)
	register("policyRef", "spec.policies", d.Spec.Policies)
	register("placementRef", "spec.placements", d.Spec.Placements)
	register("lockRef", "spec.locks", d.Spec.Locks)
	register("hookRef", "spec.hooks", d.Spec.Hooks)
	register("skillRef", "spec.skills", d.Spec.Skills)
	register("frontendRef", "spec.frontends", d.Spec.Frontends)
	register("terminalFrontendRef", "spec.frontends", d.Spec.Frontends)
	register("triggerRef", "spec.triggers", d.Spec.Triggers)
	register("sinkRef", "spec.sinks", d.Spec.Sinks)
	if err := walkReferences(reflect.ValueOf(d.Spec), "spec", targets); err != nil {
		return err
	}
	for pipelineName, pipeline := range d.Spec.Pipelines {
		for _, node := range pipeline.Nodes {
			if node.Run.HookInvoke != "" {
				if _, ok := d.Spec.Hooks[node.Run.HookInvoke]; !ok {
					return fmt.Errorf("harness: spec.pipelines.%s node %q invokes unknown hook %q", pipelineName, node.ID, node.Run.HookInvoke)
				}
			}
		}
	}
	return nil
}

func walkReferences(v reflect.Value, path string, targets map[string]referenceTarget) error {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return walkReferences(v.Elem(), path, targets)
	}
	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			yamlName := strings.Split(field.Tag.Get("yaml"), ",")[0]
			if yamlName == "" || yamlName == "-" {
				continue
			}
			fv, next := v.Field(i), path+"."+yamlName
			if strings.HasSuffix(yamlName, "Ref") || strings.HasSuffix(yamlName, "Refs") {
				base := strings.TrimSuffix(strings.TrimSuffix(yamlName, "Refs"), "Ref") + "Ref"
				if target, ok := targets[base]; ok {
					if err := checkReferenceValue(fv, next, target); err != nil {
						return err
					}
					continue
				}
			}
			if err := walkReferences(fv, next, targets); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			if key.Kind() == reflect.String {
				yamlName := key.String()
				if strings.HasSuffix(yamlName, "Ref") || strings.HasSuffix(yamlName, "Refs") {
					base := strings.TrimSuffix(strings.TrimSuffix(yamlName, "Refs"), "Ref") + "Ref"
					if target, ok := targets[base]; ok {
						if err := checkReferenceValue(v.MapIndex(key), path+"."+yamlName, target); err != nil {
							return err
						}
						continue
					}
				}
			}
			if err := walkReferences(v.MapIndex(key), path+"."+fmt.Sprint(key.Interface()), targets); err != nil {
				return err
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := walkReferences(v.Index(i), fmt.Sprintf("%s[%d]", path, i), targets); err != nil {
				return err
			}
		}
	case reflect.Interface:
		if !v.IsNil() {
			return walkReferences(v.Elem(), path, targets)
		}
	}
	return nil
}

func checkReferenceValue(v reflect.Value, path string, target referenceTarget) error {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	values := []string{}
	if v.Kind() == reflect.String {
		if v.String() != "" {
			values = append(values, v.String())
		}
	} else if v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			item := v.Index(i)
			for item.IsValid() && (item.Kind() == reflect.Interface || item.Kind() == reflect.Pointer) {
				if item.IsNil() {
					break
				}
				item = item.Elem()
			}
			if item.IsValid() && item.Kind() == reflect.String {
				values = append(values, item.String())
			}
		}
	}
	for _, name := range values {
		if _, ok := target.names[name]; !ok {
			return fmt.Errorf("harness: %s references unknown %s %q", path, target.path, name)
		}
	}
	return nil
}

func validateBashy(bashy BashyResource) error {
	if bashy.Contract == "" || bashy.RequestType == "" || bashy.ResultType == "" {
		return errors.New("harness: spec.bashy requires versioned contract, requestType and resultType")
	}
	if bashy.Execution.TimeoutMS <= 0 || bashy.Execution.MaxOutputChars <= 0 || bashy.Execution.MaxParallel <= 0 {
		return errors.New("harness: Bashy execution limits must be positive")
	}
	return nil
}

func validatePipelines(pipelines map[string]Pipeline) error {
	for name, pipeline := range pipelines {
		if pipeline.Concurrency <= 0 || len(pipeline.Nodes) == 0 {
			return fmt.Errorf("harness: pipeline %q requires positive concurrency and nodes", name)
		}
		nodes := make(map[string]Stage, len(pipeline.Nodes))
		for _, node := range pipeline.Nodes {
			if node.ID == "" {
				return fmt.Errorf("harness: pipeline %q has a node without id", name)
			}
			if _, exists := nodes[node.ID]; exists {
				return fmt.Errorf("harness: pipeline %q has duplicate node %q", name, node.ID)
			}
			nodes[node.ID] = node
			forms := 0
			if node.Run.Stage != "" {
				forms++
			}
			if node.Run.PipelineRef != "" {
				forms++
			}
			if node.Run.Repeat != nil {
				forms++
			}
			if node.Run.ForEach != nil {
				forms++
			}
			if node.Run.Switch != nil {
				forms++
			}
			if node.Run.Fallback != nil {
				forms++
			}
			if node.Run.HookInvoke != "" {
				forms++
			}
			if forms != 1 {
				return fmt.Errorf("harness: pipeline %q node %q must select exactly one run form", name, node.ID)
			}
			if node.RetryV1 != nil && node.RetryV1.MaxAttempts <= 0 {
				return fmt.Errorf("harness: pipeline %q node %q retry must be bounded", name, node.ID)
			}
			if node.Run.Repeat != nil && node.Run.Repeat.MaxIterations <= 0 {
				return fmt.Errorf("harness: pipeline %q node %q repeat must be bounded", name, node.ID)
			}
			if node.Run.ForEach != nil && node.Run.ForEach.MaxParallel <= 0 {
				return fmt.Errorf("harness: pipeline %q node %q forEach must be bounded", name, node.ID)
			}
			if node.Run.Fallback != nil && (node.Run.Fallback.MaxAttempts <= 0 || len(node.Run.Fallback.Attempts) == 0 || node.Run.Fallback.MaxAttempts > len(node.Run.Fallback.Attempts)) {
				return fmt.Errorf("harness: pipeline %q node %q fallback must be explicitly bounded by its named attempts", name, node.ID)
			}
		}
		for _, node := range pipeline.Nodes {
			for _, need := range node.Needs {
				if _, ok := nodes[need]; !ok {
					return fmt.Errorf("harness: pipeline %q node %q needs unknown node %q", name, node.ID, need)
				}
			}
		}
	}
	return validatePipelineCycles(pipelines)
}

func validatePipelineCycles(pipelines map[string]Pipeline) error {
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("harness: pipeline call cycle includes %q", name)
		}
		if visited[name] {
			return nil
		}
		visiting[name] = true
		for _, node := range pipelines[name].Nodes {
			refs := []string{node.Run.PipelineRef}
			if node.Run.Repeat != nil {
				refs = append(refs, node.Run.Repeat.PipelineRef)
			}
			if node.Run.ForEach != nil {
				refs = append(refs, node.Run.ForEach.PipelineRef)
			}
			if node.Run.Switch != nil {
				for _, item := range node.Run.Switch.Cases {
					refs = append(refs, item.PipelineRef)
				}
				refs = append(refs, node.Run.Switch.DefaultPipelineRef)
			}
			if node.Run.Fallback != nil {
				for _, item := range node.Run.Fallback.Attempts {
					refs = append(refs, item.PipelineRef)
				}
			}
			for _, ref := range refs {
				if ref != "" {
					if err := visit(ref); err != nil {
						return err
					}
				}
			}
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

func (d *Document) resolveSources() error {
	for name, source := range d.Spec.Sources {
		if (source.Text == "") == (source.File == nil) {
			return fmt.Errorf("harness: source %q must set exactly one of text or file", name)
		}
		if source.Limits.MaxBytes <= 0 {
			return fmt.Errorf("harness: source %q requires positive limits.maxBytes", name)
		}
		if source.File == nil {
			if len([]byte(source.Text)) > source.Limits.MaxBytes {
				return fmt.Errorf("harness: source %q exceeds maxBytes", name)
			}
			source.Resolved = source.Text
			digest := sha256.Sum256([]byte(source.Text))
			source.Digest = fmt.Sprintf("sha256:%x", digest)
			d.Spec.Sources[name] = source
			continue
		}
		resolved, err := d.resolvePath(source.File.Path)
		if err != nil {
			return fmt.Errorf("harness: source %q: %w", name, err)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			if !source.File.Required && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("harness: source %q: %w", name, err)
		}
		if len(data) > source.Limits.MaxBytes {
			return fmt.Errorf("harness: source %q exceeds maxBytes", name)
		}
		source.Resolved = string(data)
		digest := sha256.Sum256(data)
		source.Digest = fmt.Sprintf("sha256:%x", digest)
		d.Spec.Sources[name] = source
	}
	return nil
}

func (d *Document) resolvePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(d.BaseDir, path)
	}
	path = filepath.Clean(path)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			real = path
		} else {
			return "", err
		}
	}
	for _, root := range d.Spec.Runtime.ReadableRoots {
		if !filepath.IsAbs(root) {
			root = filepath.Join(d.BaseDir, root)
		}
		cleanRoot := filepath.Clean(root)
		if pathWithin(cleanRoot, real) {
			return real, nil
		}
		root, err = filepath.EvalSymlinks(cleanRoot)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				root = cleanRoot
			} else {
				return "", err
			}
		}
		if pathWithin(root, real) {
			return real, nil
		}
	}
	return "", fmt.Errorf("file %q is outside readableRoots", path)
}

func pathWithin(root, path string) bool {
	rel, relErr := filepath.Rel(root, path)
	return relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
