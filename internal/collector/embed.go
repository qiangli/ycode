package collector

import _ "embed"

// DefaultConfigYAML is the embedded default collector configuration.
// It uses placeholder ports that must be replaced with allocated ports
// before writing to disk via GenerateYAML.
//
//go:embed default_config.yaml
var DefaultConfigYAML string
