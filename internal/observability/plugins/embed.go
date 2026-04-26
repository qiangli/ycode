// Package plugins embeds Perses UI plugin archives for self-contained distribution.
// Plugin archives are fetched at build time via scripts/fetch-perses-plugins.sh.
package plugins

import "embed"

// ArchiveFS contains the Perses plugin .tar.gz archives under archive/.
// If no archives were fetched before building, the directory will be empty
// and the component falls back to searching the filesystem.
//
//go:embed all:archive
var ArchiveFS embed.FS
