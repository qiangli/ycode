// Package promql re-exports Prometheus PromQL engine types.
package promql

import "github.com/prometheus/prometheus/promql"

type (
	Engine     = promql.Engine
	EngineOpts = promql.EngineOpts
	Result     = promql.Result
)

var NewEngine = promql.NewEngine
