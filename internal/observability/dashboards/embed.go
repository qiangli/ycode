// Package dashboards provides embedded dashboard configurations for ycode observability.
package dashboards

import _ "embed"

// DefaultProjectJSON is the embedded default dashboard project configuration.
//
//go:embed default_project.json
var DefaultProjectJSON []byte
