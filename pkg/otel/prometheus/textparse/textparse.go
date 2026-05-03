// Package textparse re-exports Prometheus text format parser.
package textparse

import "github.com/prometheus/prometheus/model/textparse"

type ParserOptions = textparse.ParserOptions

const (
	EntrySeries    = textparse.EntrySeries
	EntryHistogram = textparse.EntryHistogram
)

var New = textparse.New
