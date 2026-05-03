// Package httpserver re-exports VictoriaMetrics HTTP server.
package httpserver

import "github.com/VictoriaMetrics/VictoriaMetrics/lib/httpserver"

type ServeOptions = httpserver.ServeOptions

var (
	Serve = httpserver.Serve
	Stop  = httpserver.Stop
)
