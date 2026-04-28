//go:build embed_source

// Package source_embed provides the ycode source archive for in-container builds.
// When the embed_source build tag is set, the source.tar.gz archive is embedded
// in the binary. This enables pulse container builds and self-healing without
// requiring Go or source code on the host.
package source_embed

import _ "embed"

//go:embed source.tar.gz
var SourceArchive []byte

// Available reports whether the source archive is embedded in this binary.
func Available() bool { return len(SourceArchive) > 0 }
