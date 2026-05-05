package features

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// Sentinel comments mark the auto-generated section in README.md (and
// anywhere else we render the same list). Both lines must exist for
// ReplaceSection to operate; if either is missing the call is a no-op
// error to prevent silently overwriting prose.
const (
	ReadmeBeginMarker = "<!-- BEGIN FEATURES -->"
	ReadmeEndMarker   = "<!-- END FEATURES -->"
)

// RenderReadmeFeatures renders the stable-tier features as a markdown bullet
// list — same format used inside the README sentinels. The output is
// deterministic (registry order preserved) so a string-equality drift check
// is meaningful.
func RenderReadmeFeatures(reg *Registry) string {
	var buf bytes.Buffer
	for _, f := range reg.ByTier(TierStable) {
		desc := f.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&buf, "- **%s** — %s\n", f.Name, desc)
	}
	return buf.String()
}

// ReplaceSection rewrites the section between BEGIN/END markers in the file
// at path. Returns (changed, error). When changed is false the file already
// matched and was not rewritten — useful as a CI drift gate.
func ReplaceSection(path, newSection string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	updated, changed, err := ReplaceSectionInBytes(data, newSection)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// ReplaceSectionInBytes is the in-memory variant — used by both ReplaceSection
// and the CI drift check. Surrounds the rendered section with one blank line
// before and after the markers (matching standard markdown spacing).
func ReplaceSectionInBytes(data []byte, newSection string) ([]byte, bool, error) {
	src := string(data)
	beginIdx := strings.Index(src, ReadmeBeginMarker)
	endIdx := strings.Index(src, ReadmeEndMarker)
	if beginIdx < 0 || endIdx < 0 {
		return nil, false, fmt.Errorf("markers %q and %q must both be present in the file", ReadmeBeginMarker, ReadmeEndMarker)
	}
	if endIdx < beginIdx {
		return nil, false, fmt.Errorf("end marker %q appears before begin marker %q", ReadmeEndMarker, ReadmeBeginMarker)
	}
	before := src[:beginIdx+len(ReadmeBeginMarker)]
	after := src[endIdx:]
	rendered := "\n" + strings.TrimRight(newSection, "\n") + "\n"
	combined := before + rendered + after
	if combined == src {
		return data, false, nil
	}
	return []byte(combined), true, nil
}
