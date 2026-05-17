package outcome

import (
	"strings"
	"testing"
)

func TestSanitizeForPublic_StripsOperatorPaths(t *testing.T) {
	in := "panic at /Users/alice/proj/foo.go line 42\nor /home/bob/work/bar.go\n"
	out := SanitizeForPublic(in)
	if strings.Contains(out, "/Users/alice") || strings.Contains(out, "/home/bob") {
		t.Fatalf("operator path not stripped:\n%s", out)
	}
	if !strings.Contains(out, "/Users/<USER>") || !strings.Contains(out, "/home/<USER>") {
		t.Fatalf("placeholder not inserted:\n%s", out)
	}
}

func TestSanitizeForPublic_RedactsTokenShapes(t *testing.T) {
	in := "GITHUB_TOKEN=ghp_abcd1234XYZ\napi_key: sk-abcd1234\nbearer my-secret-token\n"
	out := SanitizeForPublic(in)
	if strings.Contains(out, "ghp_abcd1234XYZ") || strings.Contains(out, "sk-abcd1234") || strings.Contains(out, "my-secret-token") {
		t.Fatalf("secret-shaped values leaked:\n%s", out)
	}
}

func TestParseOwnerRepo(t *testing.T) {
	cases := map[string]struct {
		owner string
		repo  string
	}{
		"https://github.com/qiangli/ycode.git":   {"qiangli", "ycode"},
		"https://github.com/qiangli/ycode":       {"qiangli", "ycode"},
		"https://github.com/alice/myfork/extras": {"alice", "myfork"},
		"https://example.com":                    {"", ""},
		"not-a-url":                              {"", ""},
	}
	for in, want := range cases {
		o, r := parseOwnerRepo(in)
		if o != want.owner || r != want.repo {
			t.Errorf("parseOwnerRepo(%q) = (%q, %q); want (%q, %q)", in, o, r, want.owner, want.repo)
		}
	}
}
