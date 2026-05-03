// Package labels re-exports Prometheus label types.
package labels

import "github.com/prometheus/prometheus/model/labels"

type (
	Labels      = labels.Labels
	SymbolTable = labels.SymbolTable
)

var NewSymbolTable = labels.NewSymbolTable
