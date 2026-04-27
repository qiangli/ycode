package tools

import (
	"testing"
)

func TestRankSkillsExactMatch(t *testing.T) {
	candidates := []ScoredSkill{
		{Name: "deploy", Description: "Deploy to production"},
		{Name: "build", Description: "Build the project"},
		{Name: "test", Description: "Run tests"},
	}

	ranked := RankSkills("build", candidates)
	if ranked[0].Name != "build" {
		t.Errorf("expected 'build' first, got %q", ranked[0].Name)
	}
}

func TestRankSkillsPartialMatch(t *testing.T) {
	candidates := []ScoredSkill{
		{Name: "deploy-staging", Description: "Deploy to staging"},
		{Name: "deploy-prod", Description: "Deploy to production"},
		{Name: "build", Description: "Build the project"},
	}

	ranked := RankSkills("deploy", candidates)
	// Both deploy skills should rank above build.
	if ranked[0].Name != "deploy-staging" && ranked[0].Name != "deploy-prod" {
		t.Errorf("expected a deploy skill first, got %q", ranked[0].Name)
	}
	if ranked[len(ranked)-1].Name != "build" {
		t.Errorf("expected 'build' last, got %q", ranked[len(ranked)-1].Name)
	}
}

func TestRankSkillsDescriptionMatch(t *testing.T) {
	candidates := []ScoredSkill{
		{Name: "ci", Description: "Run continuous integration tests"},
		{Name: "lint", Description: "Check code style"},
	}

	ranked := RankSkills("tests", candidates)
	if ranked[0].Name != "ci" {
		t.Errorf("expected 'ci' first (description match), got %q", ranked[0].Name)
	}
}

func TestRankSkillsPreservesExistingScore(t *testing.T) {
	candidates := []ScoredSkill{
		{Name: "foo", Description: "something", Score: 100.0},
		{Name: "bar", Description: "another thing", Score: 0.0},
	}

	ranked := RankSkills("unrelated", candidates)
	// "foo" should still be ranked higher due to its existing score.
	if ranked[0].Name != "foo" {
		t.Errorf("expected 'foo' first (high existing score), got %q", ranked[0].Name)
	}
}

func TestTopN(t *testing.T) {
	skills := []ScoredSkill{
		{Name: "a", Score: 3},
		{Name: "b", Score: 2},
		{Name: "c", Score: 1},
	}

	top := TopN(skills, 2)
	if len(top) != 2 {
		t.Fatalf("expected 2 results, got %d", len(top))
	}

	// Request more than available.
	top = TopN(skills, 10)
	if len(top) != 3 {
		t.Fatalf("expected 3 results, got %d", len(top))
	}
}

func TestRankSkillsEmpty(t *testing.T) {
	ranked := RankSkills("anything", nil)
	if len(ranked) != 0 {
		t.Errorf("expected empty result, got %d", len(ranked))
	}
}
