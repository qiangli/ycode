// Package vlselect re-exports VictoriaLogs select subsystem.
package vlselect

import "github.com/VictoriaMetrics/VictoriaLogs/app/vlselect"

var (
	Init           = vlselect.Init
	Stop           = vlselect.Stop
	RequestHandler = vlselect.RequestHandler
)
