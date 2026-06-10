//go:build windows

package spawncore

// execReal on Windows: no exec(2), so fork-and-wait is the only
// option — but the babysitter is the ~2MB ycode-spawn process, not
// the ~150MB ycode monolith.
func execReal(real string, args []string) int {
	return waitReal(real, args)
}
