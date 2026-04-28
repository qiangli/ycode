//go:build !embed_source

// Package source_embed provides the ycode source archive for in-container builds.
// This stub is used when the embed_source build tag is not set.
package source_embed

// SourceArchive is nil when embed_source tag is not set.
var SourceArchive []byte

// Available reports whether the source archive is embedded in this binary.
func Available() bool { return false }
