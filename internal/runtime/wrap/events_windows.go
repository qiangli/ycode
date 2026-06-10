//go:build windows

package wrap

// spawnListener / startSpawnEventListener are unix-only (unixgram
// sockets); on Windows the wrap session runs without spawn telemetry.
// The shim side (spawncore.EmitSpawn) no-ops when the env var is
// absent.
type spawnListener struct{ sockPath string }

func (l *spawnListener) stop() {}

func startSpawnEventListener(sessionDir string) (*spawnListener, error) {
	return nil, nil
}
