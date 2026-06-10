// Package spawn_embed carries the gzip-compressed ycode-spawn micro
// shim (cmd/ycode-spawn) for embedding into the ycode binary. Wrap
// sessions extract it into the per-session shim dir and point the
// tool symlinks (bash, git, grep, …) at it, so every shimmed command
// costs ~3ms and no resident memory instead of booting the ~520MB
// ycode monolith (~250ms + ~150MB resident per command).
//
// Build the embed with: scripts/embed-spawn.sh (run by `make
// compile` via the ensure-embeds chain; pure-Go stdlib build, no
// soft-skip needed). The embed_spawn build tag is auto-added by the
// Makefile's TAG_LIST probe when ycode-spawn.gz exists. Without the
// tag, Available() reports false and wrap falls back to symlinking
// shims at the ycode binary itself (wrap.ShimMain).
package spawn_embed
