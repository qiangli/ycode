//go:build experimental

package skillcnl

import (
	"reflect"
	"strings"
	"testing"
)

// sampleSkill is the canonical fixture used across roundtrip tests.
// Its dhnt identifiers all exist in testdata/glossary.yaml.
func sampleSkill() Skill {
	return Skill{
		Name: "rilease", // human picks any canonical-dhnt name
		Caps: []string{"gito", "gitohuba"},
		Steps: []Step{
			{
				Name:      "bumipiviro",
				Primitive: "gito",
				Args: []Arg{
					{Name: "ini", Value: NewRef("semivero")},
					{Name: "outo", Value: NewRef("semivero")},
				},
			},
			{
				Name:      "anini",
				Primitive: "gitohuba",
				Args: []Arg{
					{Name: "budageto", Value: NewNumber(2018)},
				},
			},
		},
	}
}

func TestLineariseDhnt_BasicShape(t *testing.T) {
	got, err := LineariseDhnt(sampleSkill())
	if err != nil {
		t.Fatalf("LineariseDhnt: %v", err)
	}
	if err := validateLayer15Charset(got); err != nil {
		t.Fatalf("LineariseDhnt produced non-Layer-1.5 output: %v\n%s", err, got)
	}
	// Every word in the output must be canonical dhnt.
	for _, w := range strings.Fields(got) {
		if !IsCanonical(w) {
			// numerals are also acceptable; check by trying decode.
			if _, err := DecodeDecimal(w); err != nil {
				t.Errorf("output word %q is neither canonical dhnt nor a numeral", w)
			}
		}
	}
}

func TestRoundtrip_SampleSkill(t *testing.T) {
	original := sampleSkill()
	enc, err := LineariseDhnt(original)
	if err != nil {
		t.Fatalf("LineariseDhnt: %v", err)
	}
	parsed, err := ParseDhnt(enc)
	if err != nil {
		t.Fatalf("ParseDhnt(%q): %v", enc, err)
	}
	if !reflect.DeepEqual(parsed, original) {
		t.Errorf("roundtrip mismatch:\n original = %#v\n parsed   = %#v\n encoded  = %s",
			original, parsed, enc)
	}
}

// TestRoundtrip_Idempotent asserts that re-linearising the parsed AST
// produces the same dhnt output — the canonical form is stable.
func TestRoundtrip_Idempotent(t *testing.T) {
	original := sampleSkill()
	enc1, err := LineariseDhnt(original)
	if err != nil {
		t.Fatalf("LineariseDhnt 1: %v", err)
	}
	parsed, err := ParseDhnt(enc1)
	if err != nil {
		t.Fatalf("ParseDhnt: %v", err)
	}
	enc2, err := LineariseDhnt(parsed)
	if err != nil {
		t.Fatalf("LineariseDhnt 2: %v", err)
	}
	if enc1 != enc2 {
		t.Errorf("non-idempotent linearisation:\n  first  = %s\n  second = %s", enc1, enc2)
	}
}

func TestParseDhnt_RejectsNonAZ(t *testing.T) {
	bad := []string{
		"sokilili Skill fini",  // capital S
		"sokilili skill1 fini", // digit
		"sokilili skill, fini", // punctuation
	}
	for _, s := range bad {
		if _, err := ParseDhnt(s); err == nil {
			t.Errorf("ParseDhnt(%q) accepted non-Layer-1.5 input", s)
		}
	}
}

func TestParseDhnt_RejectsBadStructure(t *testing.T) {
	bad := []string{
		"",                                   // empty
		"sotepo foo bar fini fini",           // missing skill keyword
		"sokilili skill",                     // missing fini
		"sokilili rilease needaso fini fini", // empty needs block
		"sokilili rilease bogus fini",        // unknown body keyword
	}
	for _, s := range bad {
		if _, err := ParseDhnt(s); err == nil {
			t.Errorf("ParseDhnt(%q) accepted malformed input", s)
		}
	}
}

func TestLineariseLang_EnglishAndChinese(t *testing.T) {
	g, err := LoadGlossary("testdata/glossary.yaml")
	if err != nil {
		t.Fatalf("LoadGlossary: %v", err)
	}
	skill := sampleSkill()

	en, err := LineariseLang(skill, g, "en")
	if err != nil {
		t.Fatalf("LineariseLang(en): %v", err)
	}
	zh, err := LineariseLang(skill, g, "zh")
	if err != nil {
		t.Fatalf("LineariseLang(zh): %v", err)
	}

	// English must use English glossary labels for known concepts.
	for _, want := range []string{"skill", "needs", "step", "git", "github", "semver", "in", "out", "budget"} {
		if !strings.Contains(en, want) {
			t.Errorf("English linearisation missing %q:\n%s", want, en)
		}
	}
	// Chinese must use Chinese characters for known concepts; foreign
	// atoms (git, github) appear verbatim in both languages.
	for _, want := range []string{"技能", "需要", "步骤", "git", "github", "语义版本", "输入", "输出", "预算"} {
		if !strings.Contains(zh, want) {
			t.Errorf("Chinese linearisation missing %q:\n%s", want, zh)
		}
	}

	// Numerals appear as decimal in both languages.
	if !strings.Contains(en, "2018") || !strings.Contains(zh, "2018") {
		t.Errorf("expected decimal numeral 2018 in both linearisations:\n en=%s\n zh=%s", en, zh)
	}

	// English and Chinese outputs must be different (this is the
	// whole point) — but their AST roundtrip is the same.
	if en == zh {
		t.Errorf("English and Chinese linearisations are identical: %s", en)
	}
}

// TestRoundtrip_ManyShapes generates a small set of valid skill
// shapes and verifies the roundtrip property holds for every one.
func TestRoundtrip_ManyShapes(t *testing.T) {
	shapes := []Skill{
		{Name: "minili", Caps: nil, Steps: nil},
		{Name: "minili", Caps: []string{"gito"}, Steps: nil},
		{
			Name: "minili",
			Caps: []string{"gito", "lilimi"},
			Steps: []Step{
				{
					Name: "alile", Primitive: "gito",
					Args: nil,
				},
			},
		},
		{
			Name: "minili",
			Steps: []Step{
				{
					Name: "alile", Primitive: "gito",
					Args: []Arg{
						{Name: "ini", Value: NewNumber(0)},
						{Name: "outo", Value: NewNumber(2018)},
					},
				},
			},
		},
	}
	for i, s := range shapes {
		enc, err := LineariseDhnt(s)
		if err != nil {
			t.Errorf("shape %d: LineariseDhnt: %v", i, err)
			continue
		}
		parsed, err := ParseDhnt(enc)
		if err != nil {
			t.Errorf("shape %d: ParseDhnt(%q): %v", i, enc, err)
			continue
		}
		// reflect.DeepEqual on slices treats nil and empty as
		// distinct; normalise both before compare.
		want := normaliseSkill(s)
		got := normaliseSkill(parsed)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("shape %d: roundtrip mismatch\n want %#v\n  got %#v\n  enc %s",
				i, want, got, enc)
		}
	}
}

// normaliseSkill makes empty slices and nil slices comparable with
// reflect.DeepEqual.
func normaliseSkill(s Skill) Skill {
	if len(s.Caps) == 0 {
		s.Caps = nil
	}
	if len(s.Steps) == 0 {
		s.Steps = nil
	}
	for i := range s.Steps {
		if len(s.Steps[i].Args) == 0 {
			s.Steps[i].Args = nil
		}
	}
	return s
}
