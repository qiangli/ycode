package shell

import (
	"errors"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Intent
		wantErr error
	}{
		// bash bare words
		{"empty", "", Intent{Kind: IntentEmpty, Raw: ""}, nil},
		{"whitespace only", "   ", Intent{Kind: IntentEmpty, Raw: "   "}, nil},
		{"plain command", "ls -al", Intent{Kind: IntentBash, Raw: "ls -al"}, nil},
		{"pipe", "ls | wc -l", Intent{Kind: IntentBash, Raw: "ls | wc -l"}, nil},
		{"path arg", "echo /tmp/foo", Intent{Kind: IntentBash, Raw: "echo /tmp/foo"}, nil},
		{"variable assign", "FOO=bar echo $FOO", Intent{Kind: IntentBash, Raw: "FOO=bar echo $FOO"}, nil},
		{"redirect", "echo hi > out", Intent{Kind: IntentBash, Raw: "echo hi > out"}, nil},
		{"function-like", "if true; then echo x; fi", Intent{Kind: IntentBash, Raw: "if true; then echo x; fi"}, nil},
		{"herestring sentinel literal", `cat <<<"/help"`, Intent{Kind: IntentBash, Raw: `cat <<<"/help"`}, nil},

		// /<word> slash command
		{"slash command", "/help", Intent{Kind: IntentSlash, Name: "help", Args: "", Raw: "/help"}, nil},
		{"slash with leading whitespace", "  /help", Intent{Kind: IntentSlash, Name: "help", Args: "", Raw: "  /help"}, nil},
		{"slash with args", "/review main..feature", Intent{Kind: IntentSlash, Name: "review", Args: "main..feature", Raw: "/review main..feature"}, nil},
		{"slash hyphenated", "/bench-instructions", Intent{Kind: IntentSlash, Name: "bench-instructions", Args: "", Raw: "/bench-instructions"}, nil},

		// /<path> bash
		{"absolute path command", "/usr/bin/ls", Intent{Kind: IntentBash, Raw: "/usr/bin/ls"}, nil},
		{"trailing slash dir", "/help/", Intent{Kind: IntentBash, Raw: "/help/"}, nil},
		{"slash with second slash", "/a/b", Intent{Kind: IntentBash, Raw: "/a/b"}, nil},

		// @<id> skill from registry
		{"skill bare id", "@review", Intent{Kind: IntentSkill, Name: "review", Args: "", Raw: "@review"}, nil},
		{"skill hyphenated", "@security-review", Intent{Kind: IntentSkill, Name: "security-review", Args: "", Raw: "@security-review"}, nil},
		{"skill with args", "@simplify foo bar", Intent{Kind: IntentSkill, Name: "simplify", Args: "foo bar", Raw: "@simplify foo bar"}, nil},

		// @<path> skill from disk
		{"skill relative path", "@./skills/foo", Intent{Kind: IntentSkillPath, Path: "./skills/foo", Args: "", Raw: "@./skills/foo"}, nil},
		{"skill absolute path", "@/abs/path/to/skill.md", Intent{Kind: IntentSkillPath, Path: "/abs/path/to/skill.md", Args: "", Raw: "@/abs/path/to/skill.md"}, nil},
		{"skill dotted name", "@foo.bar", Intent{Kind: IntentSkillPath, Path: "foo.bar", Args: "", Raw: "@foo.bar"}, nil},

		// !<text> agent shot
		{"agent shot no space", "!ping example.com", Intent{Kind: IntentAgentShot, Args: "ping example.com", Raw: "!ping example.com"}, nil},
		{"agent shot with space", "! why did that fail", Intent{Kind: IntentAgentShot, Args: "why did that fail", Raw: "! why did that fail"}, nil},
		{"agent shot leading whitespace", "  !explain", Intent{Kind: IntentAgentShot, Args: "explain", Raw: "  !explain"}, nil},

		// ?<text> agent QA
		{"agent qa", "?how do I undo last commit", Intent{Kind: IntentAgentQA, Args: "how do I undo last commit", Raw: "?how do I undo last commit"}, nil},
		{"agent qa with space", "? what is grep", Intent{Kind: IntentAgentQA, Args: "what is grep", Raw: "? what is grep"}, nil},

		// quoting wins
		{"quoted slash", `"/help"`, Intent{Kind: IntentBash, Raw: `"/help"`}, nil},
		{"quoted skill", `"@skill"`, Intent{Kind: IntentBash, Raw: `"@skill"`}, nil},
		{"single-quoted slash", `'/help'`, Intent{Kind: IntentBash, Raw: `'/help'`}, nil},
		{"escaped slash", `\/help`, Intent{Kind: IntentBash, Raw: `\/help`}, nil},

		// mid-line literal sentinels
		{"slash mid-arg", `git commit -m "/help"`, Intent{Kind: IntentBash, Raw: `git commit -m "/help"`}, nil},
		{"skill mid-arg", `echo @something`, Intent{Kind: IntentBash, Raw: `echo @something`}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Classify(tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got err=%v intent=%+v", tt.wantErr, err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Classify(%q):\n  got  %+v\n  want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestClassify_PipelineErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"slash piped", "/help | grep foo"},
		{"slash and-and", "/help && echo done"},
		{"slash or-or", "/help || echo done"},
		{"slash semicolon", "/help; echo done"},
		{"slash redirect", "/help > out.txt"},
		{"slash background", "/help &"},
		{"skill redirect", "@review > out"},
		{"skill background", "@review &"},
		{"skill semicolon", "@review; echo done"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Classify(tt.input)
			if !errors.Is(err, ErrSentinelInPipeline) {
				t.Fatalf("expected ErrSentinelInPipeline, got err=%v intent=%+v", err, got)
			}
		})
	}
}

// Pipe-to-sentinel: <bash> | @<sentinel> is allowed. The upstream bash
// runs and its stdout becomes the skill's input.
func TestClassify_PipeToSentinel(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantKind     IntentKind
		wantName     string
		wantPath     string
		wantArgs     string
		wantUpstream string
	}{
		{
			name:         "simple pipe to skill",
			input:        "ls | @summarize",
			wantKind:     IntentSkill,
			wantName:     "summarize",
			wantUpstream: "ls",
		},
		{
			name:         "pipe with args",
			input:        "cat /etc/passwd | @explain how many users",
			wantKind:     IntentSkill,
			wantName:     "explain",
			wantArgs:     "how many users",
			wantUpstream: "cat /etc/passwd",
		},
		{
			name:         "pipe to skill path",
			input:        "ls -la | @./skills/triage",
			wantKind:     IntentSkillPath,
			wantPath:     "./skills/triage",
			wantUpstream: "ls -la",
		},
		{
			name:         "two-stage pipe to skill",
			input:        "git log --oneline | head -20 | @summarize",
			wantKind:     IntentSkill,
			wantName:     "summarize",
			wantUpstream: "git log --oneline | head -20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Classify(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Kind != tt.wantKind {
				t.Errorf("kind = %v, want %v", got.Kind, tt.wantKind)
			}
			if got.Name != tt.wantName {
				t.Errorf("name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Path != tt.wantPath {
				t.Errorf("path = %q, want %q", got.Path, tt.wantPath)
			}
			if got.Args != tt.wantArgs {
				t.Errorf("args = %q, want %q", got.Args, tt.wantArgs)
			}
			if got.Upstream != tt.wantUpstream {
				t.Errorf("upstream = %q, want %q", got.Upstream, tt.wantUpstream)
			}
		})
	}
}

// Sentinel-source pipe: @<id> | <bash>. Captures the agent's output
// and pipes it into the downstream bash via stdin.
func TestClassify_SentinelSourcePipe(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantKind       IntentKind
		wantName       string
		wantPath       string
		wantArgs       string
		wantDownstream string
	}{
		{
			name:           "skill source piped to bash",
			input:          "@summarize | tee out.txt",
			wantKind:       IntentSkill,
			wantName:       "summarize",
			wantDownstream: "tee out.txt",
		},
		{
			name:           "skill path source piped to bash",
			input:          "@./skills/triage | grep ERROR",
			wantKind:       IntentSkillPath,
			wantPath:       "./skills/triage",
			wantDownstream: "grep ERROR",
		},
		{
			name:           "skill with args piped to bash",
			input:          "@explain command flow | wc -l",
			wantKind:       IntentSkill,
			wantName:       "explain",
			wantArgs:       "command flow",
			wantDownstream: "wc -l",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Classify(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Kind != tt.wantKind {
				t.Errorf("kind = %v, want %v", got.Kind, tt.wantKind)
			}
			if got.Name != tt.wantName {
				t.Errorf("name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Path != tt.wantPath {
				t.Errorf("path = %q, want %q", got.Path, tt.wantPath)
			}
			if got.Args != tt.wantArgs {
				t.Errorf("args = %q, want %q", got.Args, tt.wantArgs)
			}
			if got.Downstream != tt.wantDownstream {
				t.Errorf("downstream = %q, want %q", got.Downstream, tt.wantDownstream)
			}
		})
	}
}

func TestIntentKindString(t *testing.T) {
	cases := map[IntentKind]string{
		IntentBash:      "bash",
		IntentSlash:     "slash",
		IntentSkill:     "skill",
		IntentSkillPath: "skill-path",
		IntentAgentShot: "agent-shot",
		IntentAgentQA:   "agent-qa",
		IntentEmpty:     "empty",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("IntentKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
