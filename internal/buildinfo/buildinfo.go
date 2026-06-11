// Package buildinfo holds the binary's version identity, injected by
// the Makefile's ldflags via cmd/ycode (main.version / main.commit)
// and surfaced anywhere version reporting is needed (the serve
// /version endpoint, diagnostics). A tiny leaf package so anything
// can import it without cycles.
package buildinfo

import (
	"runtime"
	"runtime/debug"
)

var (
	version = "dev"
	commit  = "unknown"
)

// Set records the ldflags-injected identity. Called once from main.
func Set(v, c string) {
	if v != "" {
		version = v
	}
	if c != "" {
		commit = c
	}
}

// Info is the wire shape of GET /version.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"go_version"`
	VCSTime   string `json:"vcs_time,omitempty"`
	Modified  bool   `json:"vcs_modified,omitempty"`
}

// Get assembles the identity, filling VCS details from the embedded
// build info when available (covers plain `go build` binaries that
// skipped the ldflags).
func Get() Info {
	info := Info{Version: version, Commit: commit, GoVersion: runtime.Version()}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "unknown" && s.Value != "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				info.VCSTime = s.Value
			case "vcs.modified":
				info.Modified = s.Value == "true"
			}
		}
	}
	return info
}
