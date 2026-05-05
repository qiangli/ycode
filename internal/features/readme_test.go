package features

import (
	"strings"
	"testing"
)

func TestRenderReadmeFeatures(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out := RenderReadmeFeatures(reg)
	if out == "" {
		t.Fatal("rendered features is empty")
	}
	// Sanity: every stable feature appears in the output.
	for _, f := range reg.ByTier(TierStable) {
		if !strings.Contains(out, "- **"+f.Name+"**") {
			t.Errorf("stable feature %q missing from rendered output", f.Name)
		}
	}
	// Sanity: experimental/wip features must NOT appear.
	for _, f := range reg.ByTier(TierExperimental) {
		if strings.Contains(out, "- **"+f.Name+"**") {
			t.Errorf("experimental feature %q leaked into rendered output", f.Name)
		}
	}
}

func TestReplaceSectionInBytesRoundTrip(t *testing.T) {
	src := []byte(`# Title

intro

` + ReadmeBeginMarker + `
old content
` + ReadmeEndMarker + `

trailing
`)
	updated, changed, err := ReplaceSectionInBytes(src, "- **a** — alpha\n- **b** — beta\n")
	if err != nil {
		t.Fatalf("ReplaceSectionInBytes: %v", err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	want := `# Title

intro

` + ReadmeBeginMarker + `
- **a** — alpha
- **b** — beta
` + ReadmeEndMarker + `

trailing
`
	if got := string(updated); got != want {
		t.Errorf("output mismatch.\n got: %q\nwant: %q", got, want)
	}
}

func TestReplaceSectionInBytesNoChange(t *testing.T) {
	src := []byte(ReadmeBeginMarker + "\n- **a** — alpha\n" + ReadmeEndMarker + "\n")
	_, changed, err := ReplaceSectionInBytes(src, "- **a** — alpha\n")
	if err != nil {
		t.Fatalf("ReplaceSectionInBytes: %v", err)
	}
	if changed {
		t.Error("expected no change when content matches")
	}
}

func TestReplaceSectionInBytesMissingMarker(t *testing.T) {
	src := []byte("# Title\n\nno markers here\n")
	_, _, err := ReplaceSectionInBytes(src, "anything")
	if err == nil {
		t.Fatal("expected error when markers are missing")
	}
}
