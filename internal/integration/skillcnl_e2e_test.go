//go:build experimental && integration

package integration

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/skillcnl"
)

// TestSkillCNL_E2E exercises the full Phase-0 pipeline end-to-end:
//
//  1. Load the real seed glossary from the package's testdata.
//  2. Build a representative skill via the public API.
//  3. Linearise to Layer 1.5 (dhnt) — the canonical machine form.
//  4. Verify the canonical form is strictly [a-z]+whitespace.
//  5. Parse it back to AST.
//  6. Verify the AST roundtrip is exact.
//  7. Linearise to Layer 1 in English and Chinese.
//  8. Verify each language has the expected display labels.
//
// This is the integration-level "if every component agrees, the
// architecture works" test. Unit tests in the skillcnl package cover
// edge cases on each component individually.
func TestSkillCNL_E2E(t *testing.T) {
	glossaryPath := findGlossary(t)
	g, err := skillcnl.LoadGlossary(glossaryPath)
	if err != nil {
		t.Fatalf("LoadGlossary(%q): %v", glossaryPath, err)
	}
	if g.Len() == 0 {
		t.Fatal("seed glossary is empty")
	}

	original := skillcnl.Skill{
		Name: "rilease",
		Caps: []string{"gito", "gitohuba", "lilimi"},
		Steps: []skillcnl.Step{
			{
				Name:      "bumipiviro",
				Primitive: "gito",
				Args: []skillcnl.Arg{
					{Name: "ini", Value: skillcnl.NewRef("semivero")},
					{Name: "outo", Value: skillcnl.NewRef("semivero")},
				},
			},
			{
				Name:      "anini",
				Primitive: "gitohuba",
				Args: []skillcnl.Arg{
					{Name: "budageto", Value: skillcnl.NewNumber(2018)},
				},
			},
		},
	}

	dhnt, err := skillcnl.LineariseDhnt(original)
	if err != nil {
		t.Fatalf("LineariseDhnt: %v", err)
	}
	t.Logf("Layer 1.5 (dhnt): %s", dhnt)

	for i := 0; i < len(dhnt); i++ {
		c := dhnt[i]
		if c == ' ' || c == '\t' || c == '\n' {
			continue
		}
		if c < 'a' || c > 'z' {
			t.Fatalf("dhnt output contains non-[a-z] character %q at position %d", c, i)
		}
	}

	parsed, err := skillcnl.ParseDhnt(dhnt)
	if err != nil {
		t.Fatalf("ParseDhnt(%q): %v", dhnt, err)
	}
	if !reflect.DeepEqual(parsed, original) {
		t.Fatalf("AST roundtrip mismatch:\n original = %#v\n parsed   = %#v", original, parsed)
	}

	en, err := skillcnl.LineariseLang(original, g, "en")
	if err != nil {
		t.Fatalf("LineariseLang(en): %v", err)
	}
	t.Logf("Layer 1 (en): %s", en)

	zh, err := skillcnl.LineariseLang(original, g, "zh")
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
	dhnt2, err := skillcnl.LineariseDhnt(parsed)
	if err != nil {
		t.Fatalf("LineariseDhnt second pass: %v", err)
	}
	if dhnt != dhnt2 {
		t.Errorf("dhnt linearisation is not idempotent:\n  first  = %s\n  second = %s", dhnt, dhnt2)
	}
}

// findGlossary locates the seed glossary YAML by walking up from the
// current test directory. Tests run with various cwd depending on the
// runner; this gives a stable lookup.
func findGlossary(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../runtime/skillcnl/testdata/glossary.yaml",
		"../../internal/runtime/skillcnl/testdata/glossary.yaml",
		"internal/runtime/skillcnl/testdata/glossary.yaml",
	}
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
	t.Fatalf("could not locate seed glossary; tried %v from %s", candidates, wd)
	return ""
}
