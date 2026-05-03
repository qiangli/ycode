// Package vlstorage re-exports VictoriaLogs storage subsystem.
package vlstorage

import "github.com/VictoriaMetrics/VictoriaLogs/app/vlstorage"

type Storage = vlstorage.Storage

var (
	Init           = vlstorage.Init
	Stop           = vlstorage.Stop
	RequestHandler = vlstorage.RequestHandler
)
