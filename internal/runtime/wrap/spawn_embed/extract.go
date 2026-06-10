package spawn_embed

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

// compressedSpawn holds the gzip-compressed ycode-spawn binary.
// Set by embed_spawn.go (build tag) or left empty via embed_stub.go.
var compressedSpawn []byte

// Available reports whether an embedded ycode-spawn binary is present.
func Available() bool {
	return len(compressedSpawn) > 0
}

// ExtractTo gunzips the embedded ycode-spawn binary to path and marks
// it executable. Unlike the podman/runner embeds there is no SHA
// cache: the destination is the per-session shim dir, freshly created
// on every wrap invocation and removed on exit, so a plain write is
// both simpler and always correct.
func ExtractTo(path string) error {
	if !Available() {
		return fmt.Errorf("no embedded ycode-spawn (build with: scripts/embed-spawn.sh + -tags embed_spawn)")
	}
	zr, err := gzip.NewReader(bytes.NewReader(compressedSpawn))
	if err != nil {
		return fmt.Errorf("gunzip ycode-spawn: %w", err)
	}
	defer zr.Close()
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, zr); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
