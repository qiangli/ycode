//go:build embed_spawn

package spawn_embed

import _ "embed"

//go:embed ycode-spawn.gz
var embeddedSpawnGz []byte

func init() {
	compressedSpawn = embeddedSpawnGz
}
