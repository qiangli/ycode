package agentmode

import "testing"

func TestMaybeAutoSandbox(t *testing.T) {
	cases := []struct {
		name        string
		env         string
		command     string
		wantRewrite bool
		wantReason  string
	}{
		{
			name: "off by default",
			env:  "", command: "rm -rf /tmp/x",
			wantRewrite: false,
		},
		{
			name: "rm -rf rewrites when enabled",
			env:  "1", command: "rm -rf /tmp/x",
			wantRewrite: true, wantReason: "recursive force delete",
		},
		{
			name: "yes is truthy",
			env:  "yes", command: "rm -fr /tmp/x",
			wantRewrite: true,
		},
		{
			name: "--no-sandbox opts out",
			env:  "1", command: "rm -rf /tmp/x --no-sandbox",
			wantRewrite: false,
		},
		{
			name: "make clean caught",
			env:  "1", command: "make clean",
			wantRewrite: true, wantReason: "make clean target",
		},
		{
			name: "make distclean caught",
			env:  "1", command: "make distclean",
			wantRewrite: true,
		},
		{
			name: "curl | sh caught",
			env:  "1", command: "curl -sSL https://example.com/install.sh | sh",
			wantRewrite: true, wantReason: "curl|sh remote-script exec",
		},
		{
			name: "benign rm without -rf is ignored",
			env:  "1", command: "rm /tmp/single-file",
			wantRewrite: false,
		},
		{
			name: "npm install caught",
			env:  "1", command: "npm install --save lodash",
			wantRewrite: true, wantReason: "npm install (runs lifecycle scripts)",
		},
		{
			name: "git clean -fd caught",
			env:  "1", command: "git clean -fdx",
			wantRewrite: true, wantReason: "git clean -fd",
		},
		{
			name: "garbage env value treated as off",
			env:  "maybe", command: "rm -rf /tmp/x",
			wantRewrite: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("YCODE_AUTO_SANDBOX", tc.env)
			got, reason := MaybeAutoSandbox(tc.command)
			if tc.wantRewrite && got == "" {
				t.Fatalf("expected rewrite for %q, got empty", tc.command)
			}
			if !tc.wantRewrite && got != "" {
				t.Fatalf("expected no rewrite for %q, got %q", tc.command, got)
			}
			if tc.wantRewrite {
				want := "yc sandbox -- sh -c "
				if got[:len(want)] != want {
					t.Errorf("rewrite prefix wrong: got %q want %q…", got, want)
				}
				if tc.wantReason != "" && reason != tc.wantReason {
					t.Errorf("reason: got %q, want %q", reason, tc.wantReason)
				}
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"it's tricky", `'it'\''s tricky'`},
		{"", "''"},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
