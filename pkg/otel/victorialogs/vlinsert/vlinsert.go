// Package vlinsert re-exports VictoriaLogs insert subsystem.
package vlinsert

import "github.com/VictoriaMetrics/VictoriaLogs/app/vlinsert"

var (
	Init           = vlinsert.Init
	Stop           = vlinsert.Stop
	RequestHandler = vlinsert.RequestHandler
)
