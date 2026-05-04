//go:build windows

package observability

// cleanStaleFlock is a no-op on Windows.
// VictoriaLogs uses a different locking mechanism on Windows.
func cleanStaleFlock(dataDir string) {}
