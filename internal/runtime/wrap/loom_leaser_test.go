package wrap

import "testing"

func TestInferLoomLabel(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, "agent"},
		{[]string{}, "agent"},
		{[]string{""}, "agent"},
		{[]string{"claude-code"}, "claude-code"},
		{[]string{"/usr/bin/codex"}, "codex"},
		{[]string{"/usr/bin/codex", "--help"}, "codex"},
		{[]string{"opencode.exe"}, "opencode"},
	}
	for _, tc := range cases {
		got := inferLoomLabel(tc.args)
		if got != tc.want {
			t.Errorf("inferLoomLabel(%v)=%q want %q", tc.args, got, tc.want)
		}
	}
}
