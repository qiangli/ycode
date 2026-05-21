//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dhnt/dhnt/skills"
)

// TestSkillCNL_E2E exercises the full Phase-0 pipeline end-to-end:
//
//  1. Load the upstream skills package's seed glossary plus ycode's
//     domain-specific glossary, merged into one Glossary.
//  2. Build a representative skill via the public API.
//  3. Linearise to Layer 1.5 (dhnt) — the canonical machine form.
//  4. Verify the canonical form is strictly [a-z]+whitespace.
//  5. Parse it back to AST.
//  6. Verify the AST roundtrip is exact.
//  7. Linearise to Layer 1 in English and Chinese.
//  8. Verify each language has the expected display labels.
//
// This is the integration-level "if every component agrees, the
// architecture works" test. Unit tests in the upstream skills package
// cover edge cases on each component individually.
func TestSkillCNL_E2E(t *testing.T) {
	g := loadMergedGlossary(t)
	if g.Len() == 0 {
		t.Fatal("merged glossary is empty")
	}

	original := skills.Skill{
		Name: "rilease",
		Caps: []string{"gito", "gitohuba", "lilimi"},
		Steps: []skills.Step{
			{
				Name:      "bumipiviro",
				Primitive: "gito",
				Args: []skills.Arg{
					{Name: "ini", Value: skills.NewRef("semivero")},
					{Name: "outo", Value: skills.NewRef("semivero")},
				},
			},
			{
				Name:      "anini",
				Primitive: "gitohuba",
				Args: []skills.Arg{
					{Name: "budageto", Value: skills.NewNumber(2018)},
				},
			},
		},
	}

	dh, err := skills.LineariseDhnt(original)
	if err != nil {
		t.Fatalf("LineariseDhnt: %v", err)
	}
	t.Logf("Layer 1.5 (dhnt): %s", dh)

	for i := 0; i < len(dh); i++ {
		c := dh[i]
		if c == ' ' || c == '\t' || c == '\n' {
			continue
		}
		if c < 'a' || c > 'z' {
			t.Fatalf("dhnt output contains non-[a-z] character %q at position %d", c, i)
		}
	}

	parsed, err := skills.ParseDhnt(dh)
	if err != nil {
		t.Fatalf("ParseDhnt(%q): %v", dh, err)
	}
	if !reflect.DeepEqual(parsed, original) {
		t.Fatalf("AST roundtrip mismatch:\n original = %#v\n parsed   = %#v", original, parsed)
	}

	en, err := skills.LineariseLang(original, g, "en")
	if err != nil {
		t.Fatalf("LineariseLang(en): %v", err)
	}
	t.Logf("Layer 1 (en): %s", en)

	zh, err := skills.LineariseLang(original, g, "zh")
	if err != nil {
		t.Fatalf("LineariseLang(zh): %v", err)
	}
	t.Logf("Layer 1 (zh): %s", zh)

	wantEn := []string{"skill", "needs", "step", "git", "github", "in", "out", "semver", "budget", "2018"}
	for _, w := range wantEn {
		if !strings.Contains(en, w) {
			t.Errorf("English linearisation missing %q:\n%s", w, en)
		}
	}
	wantZh := []string{"技能", "需要", "步骤", "git", "github", "输入", "输出", "语义版本", "预算", "2018"}
	for _, w := range wantZh {
		if !strings.Contains(zh, w) {
			t.Errorf("Chinese linearisation missing %q:\n%s", w, zh)
		}
	}

	if en == zh {
		t.Errorf("expected en and zh linearisations to differ; both = %s", en)
	}

	// Re-linearise the parsed AST: idempotent canonical form.
	dh2, err := skills.LineariseDhnt(parsed)
	if err != nil {
		t.Fatalf("LineariseDhnt second pass: %v", err)
	}
	if dh != dh2 {
		t.Errorf("dhnt linearisation is not idempotent:\n  first  = %s\n  second = %s", dh, dh2)
	}
}

// loadMergedGlossary loads the upstream embedded seed glossary from
// github.com/dhnt/dhnt/skills, then layers ycode's domain-specific
// extensions on top via Glossary.Merge. The upstream seed is shipped
// in the module binary (no filesystem dependency); the ycode domain
// glossary lives in this tree at assets/skillcnl/ycode-glossary.yaml.
func loadMergedGlossary(t *testing.T) *skills.Glossary {
	t.Helper()
	base, err := skills.SeedGlossary()
	if err != nil {
		t.Fatalf("SeedGlossary: %v", err)
	}
	domain := findFile(t, []string{
		"../../assets/skillcnl/ycode-glossary.yaml",
		"assets/skillcnl/ycode-glossary.yaml",
	})
	addendum, err := skills.LoadGlossary(domain)
	if err != nil {
		t.Fatalf("LoadGlossary(domain %q): %v", domain, err)
	}
	merged, err := base.Merge(addendum)
	if err != nil {
		t.Fatalf("Glossary.Merge: %v", err)
	}
	return merged
}

func findFile(t *testing.T, candidates []string) string {
	t.Helper()
	wd, _ := os.Getwd()
	for _, rel := range candidates {
		p := filepath.Join(wd, rel)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, rel := range candidates {
		if _, err := os.Stat(rel); err == nil {
			return rel
		}
	}
	t.Fatalf("could not locate any of %v from %s", candidates, wd)
	return ""
}
