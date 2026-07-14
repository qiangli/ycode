package tools

import "sync"

// resetTelemetryStoreForTest clears the memoized store probe so a test can re-resolve it under
// different env. Test-only; the probe is a sync.Once in production precisely so the reachability
// check is paid once per process.
func resetTelemetryStoreForTest() {
	storeOnce = sync.Once{}
	storeClient = nil
	storeUp = false
	storeWhy = ""
}
