//go:build !embed_spawn

package spawn_embed

// No embedded ycode-spawn — Available() returns false and wrap falls
// back to symlinking shims at the ycode binary itself. Build with
// -tags embed_spawn after running: scripts/embed-spawn.sh
