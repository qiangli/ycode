package agentdef

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// multiDocSeparator is the separator used in ai-swarm style multi-document YAML.
const multiDocSeparator = "###"

// LoadDir loads all agent definitions from YAML files in the given directory.
// Returns nil (no error) if the directory does not exist.
func LoadDir(dir string) ([]*AgentDefinition, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read agent dir %s: %w", dir, err)
	}

	var defs []*AgentDefinition
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		fileDefs, err := LoadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		defs = append(defs, fileDefs...)
	}
	return defs, nil
}

// LoadFile loads agent definitions from a single YAML file.
// Supports both standard YAML documents (---) and ai-swarm style (###) separators.
func LoadFile(path string) ([]*AgentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data, path)
}

// Parse parses agent definitions from YAML bytes.
// Supports multi-document YAML with both --- and ### separators.
// Only documents containing an "agents:" or "name:" key are parsed;
// tool and model documents (from ai-swarm format) are silently skipped.
func Parse(data []byte, source string) ([]*AgentDefinition, error) {
	// Normalize ai-swarm ### separators to standard --- for the YAML decoder.
	normalized := normalizeMultiDoc(data)

	decoder := yaml.NewDecoder(bytes.NewReader(normalized))
	var defs []*AgentDefinition

	for {
		// Decode into a raw node first to detect document type without conflicts.
		var node yaml.Node
		err := decoder.Decode(&node)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", source, err)
		}

		docDefs, err := parseDocument(&node, source)
		if err != nil {
			return nil, err
		}
		defs = append(defs, docDefs...)
	}

	// Validate all definitions.
	for _, d := range defs {
		if err := d.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
	}

	return defs, nil
}

// parseDocument inspects a YAML node to determine the document type and decode accordingly.
func parseDocument(node *yaml.Node, source string) ([]*AgentDefinition, error) {
	// Detect document type by looking for key fields.
	docType := detectDocType(node)

	switch docType {
	case "swarm-agents":
		var doc swarmPackDoc
		if err := node.Decode(&doc); err != nil {
			return nil, fmt.Errorf("parse %s (swarm-agents): %w", source, err)
		}
		var defs []*AgentDefinition
		for i, ac := range doc.Agents {
			def, err := agentConfigToDefinition(ac, doc.Pack, doc.LogLevel)
			if err != nil {
				return nil, fmt.Errorf("parse %s: agent %d: %w", source, i, err)
			}
			defs = append(defs, def)
		}
		return defs, nil

	case "native-agent":
		var doc nativeAgentDoc
		if err := node.Decode(&doc); err != nil {
			return nil, fmt.Errorf("parse %s (native): %w", source, err)
		}
		return []*AgentDefinition{doc.toDefinition()}, nil

	default:
		// Skip unrecognized documents (tool kits, model sets, empty docs).
		return nil, nil
	}
}

// detectDocType inspects the YAML node's top-level keys to determine the document type.
func detectDocType(node *yaml.Node) string {
	if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		return "unknown"
	}
	mapping := node.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return "unknown"
	}

	hasKey := func(key string) bool {
		for i := 0; i < len(mapping.Content)-1; i += 2 {
			if mapping.Content[i].Value == key {
				return true
			}
		}
		return false
	}

	if hasKey("pack") && hasKey("agents") {
		return "swarm-agents"
	}
	if hasKey("name") && (hasKey("instruction") || hasKey("embed")) {
		return "native-agent"
	}
	return "unknown"
}

// swarmPackDoc represents an ai-swarm pack document with agents.
type swarmPackDoc struct {
	Pack     string        `yaml:"pack"`
	LogLevel string        `yaml:"log_level"`
	Agents   []agentConfig `yaml:"agents"`
}

// nativeAgentDoc represents a ycode native agent definition document.
type nativeAgentDoc struct {
	APIVersion  string            `yaml:"apiVersion"`
	Name        string            `yaml:"name"`
	Display     string            `yaml:"display"`
	Description string            `yaml:"description"`
	Instruction string            `yaml:"instruction"`
	Context     string            `yaml:"context"`
	Message     string            `yaml:"message"`
	Mode        string            `yaml:"mode"`
	Model       string            `yaml:"model"`
	Tools       []string          `yaml:"tools"`
	Embed       []string          `yaml:"embed"`
	Entrypoint  []string          `yaml:"entrypoint"`
	Flow        FlowType          `yaml:"flow"`
	Advices     *AdvicesConfig    `yaml:"advices"`
	Environment map[string]string `yaml:"environment"`
	Arguments   map[string]any    `yaml:"arguments"`
	MaxIter     int               `yaml:"max_iterations"`
	MaxTime     int               `yaml:"max_time"`
	Triggers    []TriggerPattern  `yaml:"triggers"`
}

func (d *nativeAgentDoc) toDefinition() *AgentDefinition {
	return &AgentDefinition{
		APIVersion:  d.APIVersion,
		Name:        d.Name,
		Display:     d.Display,
		Description: d.Description,
		Instruction: d.Instruction,
		Context:     d.Context,
		Message:     d.Message,
		Mode:        d.Mode,
		Model:       d.Model,
		Tools:       d.Tools,
		Embed:       d.Embed,
		Entrypoint:  d.Entrypoint,
		Flow:        d.Flow,
		Advices:     d.Advices,
		Environment: d.Environment,
		Arguments:   d.Arguments,
		MaxIter:     d.MaxIter,
		MaxTime:     d.MaxTime,
		Triggers:    d.Triggers,
	}
}

// agentConfig is the ai-swarm agent config format within a pack document.
type agentConfig struct {
	Name        string            `yaml:"name"`
	Display     string            `yaml:"display"`
	Description string            `yaml:"description"`
	Instruction string            `yaml:"instruction"`
	Context     string            `yaml:"context"`
	Message     string            `yaml:"message"`
	Model       string            `yaml:"model"`
	Functions   []string          `yaml:"functions"`
	Embed       []string          `yaml:"embed"`
	Entrypoint  []string          `yaml:"entrypoint"`
	Advices     *adviceConfig     `yaml:"advices"`
	Environment map[string]string `yaml:"environment"`
	Arguments   map[string]any    `yaml:"arguments"`
	MaxTurns    int               `yaml:"max_turns"`
	MaxTime     int               `yaml:"max_time"`
	LogLevel    string            `yaml:"log_level"`
}

type adviceConfig struct {
	Before []string `yaml:"before"`
	Around []string `yaml:"around"`
	After  []string `yaml:"after"`
}

// agentConfigToDefinition converts an ai-swarm agent config to a ycode AgentDefinition.
func agentConfigToDefinition(ac agentConfig, pack, _ string) (*AgentDefinition, error) {
	def := &AgentDefinition{
		APIVersion:  APIVersion,
		Name:        ac.Name,
		Display:     ac.Display,
		Description: ac.Description,
		Instruction: ac.Instruction,
		Context:     ac.Context,
		Message:     ac.Message,
		Model:       ac.Model,
		Tools:       ac.Functions,
		Embed:       ac.Embed,
		Entrypoint:  ac.Entrypoint,
		Environment: ac.Environment,
		Arguments:   ac.Arguments,
		MaxIter:     ac.MaxTurns,
		MaxTime:     ac.MaxTime,
	}

	if ac.Advices != nil {
		def.Advices = &AdvicesConfig{
			Before: ac.Advices.Before,
			Around: ac.Advices.Around,
			After:  ac.Advices.After,
		}
	}

	// Store pack name in environment for reference.
	if pack != "" {
		if def.Environment == nil {
			def.Environment = make(map[string]string)
		}
		if _, exists := def.Environment["pack"]; !exists {
			def.Environment["pack"] = pack
		}
	}

	return def, nil
}

// normalizeMultiDoc replaces ai-swarm ### separators with standard YAML --- separators.
func normalizeMultiDoc(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	var out [][]byte
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.Equal(trimmed, []byte(multiDocSeparator)) {
			out = append(out, []byte("---"))
		} else {
			out = append(out, line)
		}
	}
	return bytes.Join(out, []byte("\n"))
}

// DefaultLoadDirs returns the standard directories for agent definition loading.
// Global (~/.agents/ycode/agents/) is loaded first; project-local overrides.
func DefaultLoadDirs(projectDir string) []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".agents", "ycode", "agents"))
	}
	if projectDir != "" {
		dirs = append(dirs, filepath.Join(projectDir, ".agents", "ycode", "agents"))
	}
	return dirs
}

// LoadPaths loads agent definitions from multiple directories, in order.
// Later directories override earlier ones (by agent name).
func LoadPaths(dirs ...string) ([]*AgentDefinition, error) {
	seen := make(map[string]int) // name -> index in result
	var result []*AgentDefinition

	for _, dir := range dirs {
		defs, err := LoadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, d := range defs {
			if idx, exists := seen[d.Name]; exists {
				result[idx] = d // override
			} else {
				seen[d.Name] = len(result)
				result = append(result, d)
			}
		}
	}
	return result, nil
}
