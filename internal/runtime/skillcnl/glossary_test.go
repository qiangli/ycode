//go:build experimental

package skillcnl

import (
	"path/filepath"
	"strings"
	"testing"
)

func loadSeedGlossary(t *testing.T) *Glossary {
	t.Helper()
	g, err := LoadGlossary(filepath.Join("testdata", "glossary.yaml"))
	if err != nil {
		t.Fatalf("LoadGlossary: %v", err)
	}
	return g
}

func TestLoadGlossary_SeedFile(t *testing.T) {
	g := loadSeedGlossary(t)
	if g.Len() == 0 {
		t.Fatal("seed glossary loaded as empty")
	}
	// At minimum the structural keywords for the skill domain.
	required := []string{"sotepo", "felowu", "needaso"}
	for _, dhnt := range required {
		if g.LookupDhnt(dhnt) == nil {
			t.Errorf("seed glossary missing required entry %q", dhnt)
		}
	}
}

// TestGlossary_DhntFormsMatchEncoder enforces the central glossary
// invariant: every entry's dhnt key must equal the encoder's output
// for the entry's primary English label. Catches typos in the seed
// data and protects the parser's right to assume that
// LookupLabel("en", x).Dhnt == EncodeWord(x).
func TestGlossary_DhntFormsMatchEncoder(t *testing.T) {
	g := loadSeedGlossary(t)
	for _, e := range g.Entries() {
		enLabels, ok := e.Labels["en"]
		if !ok || len(enLabels) == 0 {
			// Foreign-atom entries with only an `all` label are
			// allowed to skip the encoder check; their dhnt is
			// derived from the all[0] label via the same rule.
			if all, ok := e.Labels[LangAll]; ok && len(all) > 0 {
				want, err := EncodeWord(strings.ToLower(all[0]))
				if err != nil {
					t.Errorf("entry %q: encode all[0]=%q: %v", e.Dhnt, all[0], err)
					continue
				}
				if e.Dhnt != want {
					t.Errorf("entry %q: dhnt does not match EncodeWord(all[0]=%q): got %q, want %q",
						e.Dhnt, all[0], e.Dhnt, want)
				}
				continue
			}
			t.Errorf("entry %q: no en or all label to derive dhnt from", e.Dhnt)
			continue
		}
		want, err := EncodeWord(strings.ToLower(enLabels[0]))
		if err != nil {
			t.Errorf("entry %q: EncodeWord(en[0]=%q): %v", e.Dhnt, enLabels[0], err)
			continue
		}
		if e.Dhnt != want {
			t.Errorf("entry %q: dhnt does not match EncodeWord(en[0]=%q): got %q, want %q",
				e.Dhnt, enLabels[0], e.Dhnt, want)
		}
	}
}

func TestGlossary_BidirectionalLookup(t *testing.T) {
	g := loadSeedGlossary(t)
	cases := []struct {
		lang, label, wantDhnt string
	}{
		{"en", "step", "sotepo"},
		{"en", "STEP", "sotepo"},     // case-insensitive
		{"en", "  step  ", "sotepo"}, // trim
		{"en", "action", "sotepo"},   // synonym
		{"zh", "步骤", "sotepo"},       // Chinese label
		{"zh", "buzhou", "sotepo"},   // Pinyin label
		{"all", "git", "gito"},       // foreign atom via all
		{"en", "git", "gito"},        // foreign atom falls back to all
	}
	for _, tc := range cases {
		e := g.LookupLabel(tc.lang, tc.label)
		if e == nil {
			t.Errorf("LookupLabel(%q, %q) returned nil", tc.lang, tc.label)
			continue
		}
		if e.Dhnt != tc.wantDhnt {
			t.Errorf("LookupLabel(%q, %q) = %q, want %q", tc.lang, tc.label, e.Dhnt, tc.wantDhnt)
		}
	}
}

func TestGlossary_RejectsDuplicateDhnt(t *testing.T) {
	yaml := `entries:
  - dhnt: sotepo
    kind: keyword
    labels: {en: [step]}
  - dhnt: sotepo
    kind: keyword
    labels: {en: [stomp]}
`
	if _, err := ParseGlossary([]byte(yaml)); err == nil {
		t.Error("expected duplicate dhnt key to be rejected")
	}
}

func TestGlossary_RejectsNonCanonicalDhnt(t *testing.T) {
	yaml := `entries:
  - dhnt: GIT
    kind: capability
    labels: {all: [git]}
`
	if _, err := ParseGlossary([]byte(yaml)); err == nil {
		t.Error("expected non-canonical dhnt key to be rejected")
	}
}

func TestEntry_PrimaryLabel(t *testing.T) {
	e := &Entry{
		Dhnt: "sotepo",
		Kind: KindKeyword,
		Labels: map[string][]string{
			"en": {"step", "action"},
			"zh": {"步骤", "buzhou"},
		},
	}
	if got := e.PrimaryLabel("en"); got != "step" {
		t.Errorf("PrimaryLabel(en) = %q, want %q", got, "step")
	}
	if got := e.PrimaryLabel("zh"); got != "步骤" {
		t.Errorf("PrimaryLabel(zh) = %q, want %q", got, "步骤")
	}
	if got := e.PrimaryLabel("xx"); got != "" {
		t.Errorf("PrimaryLabel(xx) = %q, want empty", got)
	}
	// fall back to LangAll
	e.Labels = map[string][]string{LangAll: {"git"}}
	if got := e.PrimaryLabel("en"); got != "git" {
		t.Errorf("PrimaryLabel(en) with all-only = %q, want %q", got, "git")
	}
}
