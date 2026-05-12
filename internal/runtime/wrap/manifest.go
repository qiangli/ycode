package wrap

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ReadServeManifest reads the on-disk manifest a running `ycode serve`
// wrote to ~/.agents/ycode/manifest.json and returns the gRPC OTLP
// endpoint operators should use for collector connection. Empty
// return value means either no serve is running, the manifest is
// malformed, or it does not advertise the endpoint — all three are
// indistinguishable on purpose: wrap's exporter bootstrap falls back
// to file-only mode in any of them.
//
// Kept private to wrap for now (no public reader exists in
// cmd/ycode/manifest.go per exploration). Promote to a shared
// package when a second non-serve caller appears.
//
// Schema follows the writer in cmd/ycode/manifest.go: an
// `endpoints` map of name → value. We only care about `otlpGRPC`
// today; other consumers will likely want `otlpHTTP` too, but the
// reader can grow when that demand is concrete.
func ReadServeManifest() (otlpGRPC string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return readServeManifestAt(filepath.Join(home, ".agents", "ycode", "manifest.json"))
}

// readServeManifestAt is the test-friendly variant. Callers pass the
// absolute manifest path; production code goes through ReadServeManifest
// which derives the path from the user's home dir.
func readServeManifestAt(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var doc struct {
		Endpoints map[string]any `json:"endpoints"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", false
	}
	v, ok := doc.Endpoints["otlpGRPC"].(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
