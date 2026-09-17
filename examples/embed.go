// Package examples exposes the canonical authored harness for CLI discovery.
package examples

import _ "embed"

// agent is the same YAML document shipped as examples/agent.yaml.
// It supplies the command tree for offline help when no project file exists.
// Executing a configured operation still requires the selected project file.
//
//go:embed agent.yaml
var agent string

// Agent returns an independent copy of the authored document.
func Agent() []byte { return []byte(agent) }
