// Package specgen re-exports Podman spec generation types.
package specgen

import "go.podman.io/podman/v6/pkg/specgen"

type (
	SpecGenerator    = specgen.SpecGenerator
	PodSpecGenerator = specgen.PodSpecGenerator
	PodBasicConfig   = specgen.PodBasicConfig
)

var NewSpecGenerator = specgen.NewSpecGenerator
