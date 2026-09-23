package spec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func (d *Document) resolveImports() error {
	d.ensureResourceMaps()
	names := make([]string, 0, len(d.Spec.Imports))
	for name := range d.Spec.Imports {
		names = append(names, name)
	}
	sort.Strings(names)
	seenNamespaces := make(map[string]string)
	for _, importName := range names {
		declaration := d.Spec.Imports[importName]
		if declaration.Source == "" || declaration.Namespace == "" || !resourceNamePattern.MatchString(declaration.Namespace) || len(declaration.Exports) == 0 {
			return fmt.Errorf("harness: import %q requires source, valid namespace and selected exports", importName)
		}
		if previous, exists := seenNamespaces[declaration.Namespace]; exists {
			return fmt.Errorf("harness: imports %q and %q collide on namespace %q", previous, importName, declaration.Namespace)
		}
		seenNamespaces[declaration.Namespace] = importName
		path, err := d.resolvePath(declaration.Source)
		if err != nil {
			return fmt.Errorf("harness: import %q: %w", importName, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("harness: import %q: %w", importName, err)
		}
		actual := sha256.Sum256(data)
		want, err := parseSHA256(declaration.Digest)
		if err != nil {
			return fmt.Errorf("harness: import %q: %w", importName, err)
		}
		if actual != want {
			return fmt.Errorf("harness: import %q digest mismatch: got sha256:%x", importName, actual)
		}
		fragment, err := decodeImport(path, data)
		if err != nil {
			return fmt.Errorf("harness: import %q: %w", importName, err)
		}
		if fragment.Spec.Runtime.DefaultAgentRef != "" || fragment.Spec.Runtime.Workspace != "" || len(fragment.Spec.Imports) != 0 {
			return fmt.Errorf("harness: import %q cannot contribute runtime defaults or nested imports", importName)
		}
		selected := make(map[string]struct{}, len(declaration.Exports))
		for _, export := range declaration.Exports {
			if _, duplicate := selected[export]; duplicate {
				return fmt.Errorf("harness: import %q selects duplicate export %q", importName, export)
			}
			selected[export] = struct{}{}
		}
		all := fragment.resourceNames()
		for _, export := range declaration.Exports {
			if _, ok := all[export]; !ok {
				return fmt.Errorf("harness: import %q selects unknown export %q", importName, export)
			}
		}
		for _, export := range declaration.Exports {
			if err := d.mergeImported(export, declaration.Namespace, fragment, selected, all, filepath.Dir(path)); err != nil {
				return fmt.Errorf("harness: import %q: %w", importName, err)
			}
		}
	}
	return nil
}

func (d *Document) ensureResourceMaps() {
	if d.Spec.Sources == nil {
		d.Spec.Sources = make(map[string]Source)
	}
	if d.Spec.Providers == nil {
		d.Spec.Providers = make(map[string]Provider)
	}
	if d.Spec.Models == nil {
		d.Spec.Models = make(map[string]Model)
	}
	if d.Spec.Routes == nil {
		d.Spec.Routes = make(map[string]Route)
	}
	if d.Spec.Contexts == nil {
		d.Spec.Contexts = make(map[string]Context)
	}
	if d.Spec.Memories == nil {
		d.Spec.Memories = make(map[string]Memory)
	}
	if d.Spec.Queues == nil {
		d.Spec.Queues = make(map[string]Queue)
	}
	if d.Spec.Sessions == nil {
		d.Spec.Sessions = make(map[string]Session)
	}
	if d.Spec.Lifecycles == nil {
		d.Spec.Lifecycles = make(map[string]Lifecycle)
	}
	if d.Spec.Policies == nil {
		d.Spec.Policies = make(map[string]Policy)
	}
	if d.Spec.Placements == nil {
		d.Spec.Placements = make(map[string]Placement)
	}
	if d.Spec.Locks == nil {
		d.Spec.Locks = make(map[string]Lock)
	}
	if d.Spec.Hooks == nil {
		d.Spec.Hooks = make(map[string]Hook)
	}
	if d.Spec.Skills == nil {
		d.Spec.Skills = make(map[string]Skill)
	}
	if d.Spec.Pipelines == nil {
		d.Spec.Pipelines = make(map[string]Pipeline)
	}
	if d.Spec.Agents == nil {
		d.Spec.Agents = make(map[string]Agent)
	}
	if d.Spec.Frontends == nil {
		d.Spec.Frontends = make(map[string]Frontend)
	}
	if d.Spec.Triggers == nil {
		d.Spec.Triggers = make(map[string]Trigger)
	}
	if d.Spec.Sinks == nil {
		d.Spec.Sinks = make(map[string]Sink)
	}
}

func decodeImport(source string, data []byte) (*Document, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return nil, located(source, fmt.Errorf("decode imported harness: %w", err))
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, located(source, errors.New("import must contain exactly one YAML document"))
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, located(source, err)
	}
	if err := inspectNode(&node); err != nil {
		return nil, located(source, err)
	}
	if doc.APIVersion != APIVersion || doc.Kind != Kind {
		return nil, located(source, errors.New("import has incompatible apiVersion or kind"))
	}
	if len(doc.Spec.Defaults) != 0 {
		return nil, located(source, errors.New("import cannot contribute global defaults"))
	}
	doc.Source, doc.BaseDir = source, filepath.Dir(source)
	return &doc, nil
}

func parseSHA256(value string) ([32]byte, error) {
	var result [32]byte
	if !strings.HasPrefix(value, "sha256:") {
		return result, errors.New("digest must use sha256:<hex>")
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	if err != nil || len(raw) != sha256.Size {
		return result, errors.New("digest must contain exactly 64 hexadecimal characters")
	}
	copy(result[:], raw)
	return result, nil
}

func (d *Document) resourceNames() map[string]struct{} {
	result := make(map[string]struct{})
	add := func(kind string, values any) {
		raw, _ := json.Marshal(values)
		var object map[string]json.RawMessage
		_ = json.Unmarshal(raw, &object)
		for name := range object {
			result[kind+"/"+name] = struct{}{}
		}
	}
	add("sources", d.Spec.Sources)
	add("providers", d.Spec.Providers)
	add("models", d.Spec.Models)
	add("routes", d.Spec.Routes)
	add("contexts", d.Spec.Contexts)
	add("memories", d.Spec.Memories)
	add("queues", d.Spec.Queues)
	add("sessions", d.Spec.Sessions)
	add("lifecycles", d.Spec.Lifecycles)
	add("policies", d.Spec.Policies)
	add("placements", d.Spec.Placements)
	add("locks", d.Spec.Locks)
	add("hooks", d.Spec.Hooks)
	add("skills", d.Spec.Skills)
	add("pipelines", d.Spec.Pipelines)
	add("agents", d.Spec.Agents)
	add("frontends", d.Spec.Frontends)
	add("triggers", d.Spec.Triggers)
	add("sinks", d.Spec.Sinks)
	return result
}

func (d *Document) mergeImported(export, namespace string, fragment *Document, selected, all map[string]struct{}, importDir string) error {
	parts := strings.Split(export, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid export %q; expected resource-kind/name", export)
	}
	kind, name, targetName := parts[0], parts[1], namespace+"."+parts[1]
	clone := func(input, output any) error {
		data, err := yaml.Marshal(input)
		if err != nil {
			return err
		}
		var node yaml.Node
		if err := yaml.Unmarshal(data, &node); err != nil {
			return err
		}
		if err := namespaceReferenceNode(&node, namespace, selected, all); err != nil {
			return err
		}
		return node.Decode(output)
	}
	collision := func(exists bool) error {
		if exists {
			return fmt.Errorf("resource collision at %s/%s", kind, targetName)
		}
		return nil
	}
	switch kind {
	case "sources":
		if _, ok := d.Spec.Sources[targetName]; ok {
			return collision(true)
		}
		value := fragment.Spec.Sources[name]
		if value.File != nil {
			path := value.File.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(importDir, path)
			}
			resolved, err := d.resolvePath(path)
			if err != nil {
				return err
			}
			content, err := os.ReadFile(resolved)
			if err != nil {
				return err
			}
			if len(content) > value.Limits.MaxBytes {
				return fmt.Errorf("source %q exceeds maxBytes", export)
			}
			value.Text, value.File = string(content), nil
		}
		d.Spec.Sources[targetName] = value
	case "providers":
		var value Provider
		if err := clone(fragment.Spec.Providers[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Providers[targetName]; ok {
			return collision(true)
		}
		d.Spec.Providers[targetName] = value
	case "models":
		var value Model
		if err := clone(fragment.Spec.Models[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Models[targetName]; ok {
			return collision(true)
		}
		d.Spec.Models[targetName] = value
	case "routes":
		var value Route
		if err := clone(fragment.Spec.Routes[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Routes[targetName]; ok {
			return collision(true)
		}
		d.Spec.Routes[targetName] = value
	case "contexts":
		var value Context
		if err := clone(fragment.Spec.Contexts[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Contexts[targetName]; ok {
			return collision(true)
		}
		d.Spec.Contexts[targetName] = value
	case "memories":
		var value Memory
		if err := clone(fragment.Spec.Memories[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Memories[targetName]; ok {
			return collision(true)
		}
		d.Spec.Memories[targetName] = value
	case "queues":
		var value Queue
		if err := clone(fragment.Spec.Queues[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Queues[targetName]; ok {
			return collision(true)
		}
		d.Spec.Queues[targetName] = value
	case "sessions":
		var value Session
		if err := clone(fragment.Spec.Sessions[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Sessions[targetName]; ok {
			return collision(true)
		}
		d.Spec.Sessions[targetName] = value
	case "lifecycles":
		var value Lifecycle
		if err := clone(fragment.Spec.Lifecycles[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Lifecycles[targetName]; ok {
			return collision(true)
		}
		d.Spec.Lifecycles[targetName] = value
	case "policies":
		var value Policy
		if err := clone(fragment.Spec.Policies[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Policies[targetName]; ok {
			return collision(true)
		}
		d.Spec.Policies[targetName] = value
	case "placements":
		var value Placement
		if err := clone(fragment.Spec.Placements[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Placements[targetName]; ok {
			return collision(true)
		}
		d.Spec.Placements[targetName] = value
	case "locks":
		var value Lock
		if err := clone(fragment.Spec.Locks[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Locks[targetName]; ok {
			return collision(true)
		}
		d.Spec.Locks[targetName] = value
	case "hooks":
		var value Hook
		if err := clone(fragment.Spec.Hooks[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Hooks[targetName]; ok {
			return collision(true)
		}
		d.Spec.Hooks[targetName] = value
	case "skills":
		var value Skill
		if err := clone(fragment.Spec.Skills[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Skills[targetName]; ok {
			return collision(true)
		}
		d.Spec.Skills[targetName] = value
	case "pipelines":
		var value Pipeline
		if err := clone(fragment.Spec.Pipelines[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Pipelines[targetName]; ok {
			return collision(true)
		}
		d.Spec.Pipelines[targetName] = value
	case "agents":
		var value Agent
		if err := clone(fragment.Spec.Agents[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Agents[targetName]; ok {
			return collision(true)
		}
		d.Spec.Agents[targetName] = value
	case "frontends":
		var value Frontend
		if err := clone(fragment.Spec.Frontends[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Frontends[targetName]; ok {
			return collision(true)
		}
		d.Spec.Frontends[targetName] = value
	case "triggers":
		var value Trigger
		if err := clone(fragment.Spec.Triggers[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Triggers[targetName]; ok {
			return collision(true)
		}
		d.Spec.Triggers[targetName] = value
	case "sinks":
		var value Sink
		if err := clone(fragment.Spec.Sinks[name], &value); err != nil {
			return err
		}
		if _, ok := d.Spec.Sinks[targetName]; ok {
			return collision(true)
		}
		d.Spec.Sinks[targetName] = value
	default:
		return fmt.Errorf("unsupported export kind %q", kind)
	}
	return nil
}

func namespaceReferenceNode(node *yaml.Node, namespace string, selected, all map[string]struct{}) error {
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i].Value, node.Content[i+1]
			kind := resourceKindForRef(key)
			if key == "hook.invoke" {
				kind = "hooks"
			}
			if kind != "" {
				values := []*yaml.Node{value}
				if value.Kind == yaml.SequenceNode {
					values = value.Content
				}
				for _, item := range values {
					id := kind + "/" + item.Value
					if _, local := all[id]; !local {
						return fmt.Errorf("imported resource references external %s", id)
					}
					if _, exported := selected[id]; !exported {
						return fmt.Errorf("imported resource references non-exported %s", id)
					}
					item.Value = namespace + "." + item.Value
				}
			} else if err := namespaceReferenceNode(value, namespace, selected, all); err != nil {
				return err
			}
		}
	} else {
		for _, child := range node.Content {
			if err := namespaceReferenceNode(child, namespace, selected, all); err != nil {
				return err
			}
		}
	}
	return nil
}

// CanonicalJSON is a deterministic, secret-safe compiled graph dump.
func (d *Document) CanonicalJSON() ([]byte, error) { return json.Marshal(d) }
func (d *Document) graphDigest() string {
	data, _ := d.CanonicalJSON()
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
