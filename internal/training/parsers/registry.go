package parsers

import "fmt"

var registry = map[string]Parser{
	"hermes": &HermesParser{},
	"json":   &JSONParser{},
}

// Get returns a parser by name.
func Get(name string) (Parser, error) {
	p, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown parser: %q", name)
	}
	return p, nil
}

// Register adds a parser to the registry.
func Register(p Parser) {
	registry[p.Name()] = p
}

// List returns all registered parser names.
func List() []string {
	var names []string
	for name := range registry {
		names = append(names, name)
	}
	return names
}
