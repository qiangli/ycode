package spec

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// defaultResourceSections are maps of named resources. A defaults entry for
// one of these sections is a template applied to each declared resource of
// that kind; it does not create resources by itself.
var defaultResourceSections = map[string]struct{}{
	"sources": {}, "providers": {}, "models": {}, "routes": {}, "contexts": {},
	"memories": {}, "queues": {}, "sessions": {}, "lifecycles": {}, "policies": {},
	"placements": {}, "locks": {}, "hooks": {}, "skills": {}, "pipelines": {},
	"agents": {}, "frontends": {}, "triggers": {}, "sinks": {},
}

var defaultableSections = map[string]struct{}{
	"interfaces": {}, "runtime": {}, "bashy": {}, "observability": {},
	"sources": {}, "providers": {}, "models": {}, "routes": {}, "contexts": {},
	"memories": {}, "queues": {}, "sessions": {}, "lifecycles": {}, "policies": {},
	"placements": {}, "locks": {}, "hooks": {}, "skills": {}, "pipelines": {},
	"agents": {}, "frontends": {}, "triggers": {}, "sinks": {}, "extensions": {},
}

// applyYAMLDefaults applies spec.defaults to the explicit section values in
// the YAML tree. Maps merge recursively, sequences and scalars replace, and a
// local null replaces an inherited value with null. A defaults template for a
// named-resource section applies to each resource, not to the resource map.
func applyYAMLDefaults(root *yaml.Node) error {
	doc := root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	specNode := mappingValue(doc, "spec")
	if specNode == nil {
		return nil
	}
	defaults := mappingValue(specNode, "defaults")
	if defaults == nil || isNull(defaults) {
		return nil
	}
	if defaults.Kind != yaml.MappingNode {
		return fmt.Errorf("harness: spec.defaults must be a mapping or null")
	}
	for i := 0; i < len(defaults.Content); i += 2 {
		key, template := defaults.Content[i], defaults.Content[i+1]
		section := key.Value
		if _, ok := defaultableSections[section]; !ok {
			return fmt.Errorf("harness: spec.defaults has unknown section %q", section)
		}
		if isNull(template) {
			continue
		}
		local := mappingValue(specNode, section)
		if _, isResourceMap := defaultResourceSections[section]; isResourceMap {
			if template.Kind != yaml.MappingNode {
				return fmt.Errorf("harness: spec.defaults.%s must be a mapping or null", section)
			}
			if local == nil || isNull(local) {
				if len(template.Content) != 0 {
					return fmt.Errorf("harness: spec.defaults.%s has no declared resources to inherit it", section)
				}
				continue
			}
			if local.Kind != yaml.MappingNode {
				return fmt.Errorf("harness: spec.%s must be a mapping or null", section)
			}
			for j := 0; j < len(local.Content); j += 2 {
				resource := local.Content[j+1]
				if isNull(resource) {
					continue
				}
				merged, err := mergeYAML(template, resource)
				if err != nil {
					return fmt.Errorf("harness: defaults for spec.%s.%s: %w", section, local.Content[j].Value, err)
				}
				local.Content[j+1] = merged
			}
			continue
		}
		if local != nil && isNull(local) {
			continue
		}
		if local == nil {
			local = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
		}
		merged, err := mergeYAML(template, local)
		if err != nil {
			return fmt.Errorf("harness: defaults for spec.%s: %w", section, err)
		}
		setMappingValue(specNode, section, merged)
	}
	removeNullResources(specNode)
	return nil
}

func removeNullResources(specNode *yaml.Node) {
	for section := range defaultResourceSections {
		resources := mappingValue(specNode, section)
		if resources == nil || resources.Kind != yaml.MappingNode {
			continue
		}
		kept := resources.Content[:0]
		for i := 0; i+1 < len(resources.Content); i += 2 {
			if isNull(resources.Content[i+1]) {
				continue
			}
			kept = append(kept, resources.Content[i], resources.Content[i+1])
		}
		resources.Content = kept
	}
}

func mergeYAML(base, override *yaml.Node) (*yaml.Node, error) {
	if base == nil {
		return cloneYAML(override), nil
	}
	if override == nil || isNull(override) {
		return cloneYAML(override), nil
	}
	if base.Kind != yaml.MappingNode || override.Kind != yaml.MappingNode {
		return cloneYAML(override), nil
	}
	result := cloneYAML(base)
	for i := 0; i < len(override.Content); i += 2 {
		key, value := override.Content[i], override.Content[i+1]
		found := -1
		for j := 0; j < len(result.Content); j += 2 {
			if result.Content[j].Value == key.Value {
				found = j
				break
			}
		}
		if found < 0 {
			result.Content = append(result.Content, cloneYAML(key), cloneYAML(value))
			continue
		}
		merged, err := mergeYAML(result.Content[found+1], value)
		if err != nil {
			return nil, err
		}
		result.Content[found+1] = merged
	}
	return result, nil
}

func mappingValue(node *yaml.Node, name string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}

func setMappingValue(node *yaml.Node, name string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, value)
}

func isNull(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!null"
}

func cloneYAML(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	copy := *node
	copy.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		copy.Content[i] = cloneYAML(child)
	}
	return &copy
}
