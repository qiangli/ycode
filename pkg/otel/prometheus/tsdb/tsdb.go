// Package tsdb re-exports Prometheus TSDB types.
package tsdb

import "github.com/prometheus/prometheus/tsdb"

type (
	DB      = tsdb.DB
	Options = tsdb.Options
)

var Open = tsdb.Open
