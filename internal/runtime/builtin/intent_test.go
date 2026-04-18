package builtin

import "testing"

func TestDetectIntent_CommitMatches(t *testing.T) {
	tests := []struct {
		input    string
		wantOp   string
		wantArgs string
	}{
		// Direct imperatives
		{"commit", "commit", ""},
		{"commit changes", "commit", ""},
		{"commit my changes", "commit", ""},
		{"commit the changes", "commit", ""},
		{"commit these changes", "commit", ""},
		{"commit all changes", "commit", ""},
		{"Commit", "commit", ""},
		{"COMMIT", "commit", ""},

		// With hint context
		{"commit my changes for the auth fix", "commit", "for the auth fix"},
		{"commit the changes to fix login", "commit", "to fix login"},

		// Polite forms
		{"please commit", "commit", ""},
		{"please commit my changes", "commit", ""},

		// Make/create forms
		{"make a commit", "commit", ""},
		{"create a commit", "commit", ""},
		{"create commit", "commit", ""},

		// Stage and commit
		{"stage and commit", "commit", ""},
		{"add and commit", "commit", ""},

		// Pronouns
		{"commit this", "commit", ""},
		{"commit that", "commit", ""},
		{"commit it", "commit", ""},
		{"commit everything", "commit", ""},

		// Prefix phrases
		{"ok commit", "commit", ""},
		{"okay commit", "commit", ""},
		{"go ahead and commit", "commit", ""},
		{"now commit", "commit", ""},
		{"now commit my changes", "commit", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			m := DetectIntent(tt.input)
			if m == nil {
				t.Fatalf("expected match for %q, got nil", tt.input)
			}
			if m.Operation != tt.wantOp {
				t.Errorf("operation = %q, want %q", m.Operation, tt.wantOp)
			}
			if m.Args != tt.wantArgs {
				t.Errorf("args = %q, want %q", m.Args, tt.wantArgs)
			}
		})
	}
}

func TestDetectIntent_CommitRejects(t *testing.T) {
	rejects := []string{
		// Questions
		"what is a commit?",
		"how do I commit?",
		"why did the commit fail?",
		"can you explain commits?",

		// History/log requests
		"show the last commit",
		"show me the commit history",
		"view recent commits",
		"git log",
		"git show",

		// Revert/undo
		"revert the last commit",
		"undo the commit",
		"amend the commit",
		"squash the commits",

		// Explain/describe
		"explain this commit",
		"describe the commit changes",

		// Commit metadata queries
		"what was the commit hash",
		"show the commit message",
		"commit log",

		// Complex sentences that mention commit but aren't commit requests
		"I think there's a bug in the last commit",
		"tell me about the previous commit",
		"the commit diff looks wrong",

		// Empty
		"",
		"  ",

		// Unrelated
		"fix the bug in auth.go",
		"run the tests",
		"hello",
		"write a function to parse JSON",
	}

	for _, input := range rejects {
		t.Run(input, func(t *testing.T) {
			m := DetectIntent(input)
			if m != nil {
				t.Errorf("expected nil for %q, got operation=%q args=%q", input, m.Operation, m.Args)
			}
		})
	}
}

func TestDetectIntent_EdgeCases(t *testing.T) {
	// These are borderline cases that should NOT trigger to be safe.
	edgeCases := []string{
		"I've finished the feature, can you commit it?", // question mark
		"commit the last 3 files and also run tests",    // too complex
		"cherry-pick that commit",                       // negative pattern
	}

	for _, input := range edgeCases {
		t.Run(input, func(t *testing.T) {
			m := DetectIntent(input)
			if m != nil {
				t.Errorf("edge case %q should not match, got operation=%q", input, m.Operation)
			}
		})
	}
}

func TestIsQuestion(t *testing.T) {
	questions := []string{
		"what is this?",
		"how do I fix it?",
		"why is this broken?",
		"can you explain this",
		"tell me about the code",
		"does this work?",
	}
	for _, q := range questions {
		if !isQuestion(q) {
			t.Errorf("expected %q to be detected as question", q)
		}
	}

	nonQuestions := []string{
		"commit my changes",
		"fix the bug",
		"run tests",
	}
	for _, nq := range nonQuestions {
		if isQuestion(nq) {
			t.Errorf("expected %q to NOT be detected as question", nq)
		}
	}
}
