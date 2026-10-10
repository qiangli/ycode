package spec

import (
	"strings"
	"testing"
)

// Sprint 412 Story #896: reasoning effort is model policy, so it is authored
// on the model resource and nowhere else.
func TestCompileAcceptsDeclaredModelEffort(t *testing.T) {
	for _, effort := range ModelEfforts {
		fixture := strings.Replace(string(readFixture(t)), "      id: gpt-5.6", "      id: gpt-5.6\n      effort: "+effort, 1)
		doc, err := Compile(fixturePath(), []byte(fixture))
		if err != nil {
			t.Fatalf("effort %q: %v", effort, err)
		}
		if doc.Spec.Models["primary"].Effort != effort {
			t.Fatalf("effort %q was not compiled: %q", effort, doc.Spec.Models["primary"].Effort)
		}
	}
}

// An undeclared effort value is a typo, not a provider pass-through: the
// schema is closed and compilation fails.
func TestCompileRejectsUnknownModelEffort(t *testing.T) {
	fixture := strings.Replace(string(readFixture(t)), "      id: gpt-5.6", "      id: gpt-5.6\n      effort: maximum", 1)
	_, err := Compile(fixturePath(), []byte(fixture))
	if err == nil || !strings.Contains(err.Error(), "spec.models.primary.effort") {
		t.Fatalf("error = %v", err)
	}
}

// Effort is optional: the canonical fixture declares none and still compiles.
func TestCompileAllowsModelWithoutEffort(t *testing.T) {
	doc, err := Load(fixturePath())
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Spec.Models["primary"].Effort; got != "" {
		t.Fatalf("effort = %q, want empty", got)
	}
}
