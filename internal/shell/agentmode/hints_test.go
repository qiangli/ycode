package agentmode

import (
	"strings"
	"sync"
	"testing"

	"github.com/qiangli/ycode/internal/shell"
)

// fakeMetricsSink records every observation so the test can assert
// counter increments without standing up real OTel infrastructure.
type fakeMetricsSink struct {
	mu     sync.Mutex
	hints  []hintObs
	mine   []mineObs
	intent []string
	dur    []durObs
}

type hintObs struct{ id, category, phase string }
type mineObs struct{ phase, outcome string }
type durObs struct {
	kind string
	ms   float64
}

func (s *fakeMetricsSink) ObserveHint(id, category, phase string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hints = append(s.hints, hintObs{id, category, phase})
}
func (s *fakeMetricsSink) ObserveMineWrite(phase, outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mine = append(s.mine, mineObs{phase, outcome})
}
func (s *fakeMetricsSink) ObserveIntent(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intent = append(s.intent, kind)
}
func (s *fakeMetricsSink) ObserveCommandDuration(kind string, ms float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dur = append(s.dur, durObs{kind, ms})
}

// installFakeMetrics swaps in a recording sink and registers cleanup.
// Tests that need to assert metric emissions wrap their body with this.
func installFakeMetrics(t *testing.T) *fakeMetricsSink {
	t.Helper()
	t.Setenv(envMineDisable, "1")
	s := &fakeMetricsSink{}
	shell.SetMetrics(s)
	t.Cleanup(func() { shell.SetMetrics(nil) })
	return s
}

// TestSuggestSubstitutes pins the contract that suggestion strings are
// run through Pattern.ExpandString — `$1` etc. must be filled from
// capture groups, not surface as literal text.
func TestSuggestSubstitutes(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantID  string
		wantSub string // substring required in the rendered message
	}{
		{
			name:    "git verb captured into yc git",
			command: "git status",
			wantID:  "git-log-status-diff-suggests-yc-git",
			wantSub: "yc git status",
		},
		{
			name:    "git log",
			command: "git log --oneline -5",
			wantID:  "git-log-status-diff-suggests-yc-git",
			wantSub: "yc git log",
		},
		{
			name:    "grep on source file with pipe-bearing regex",
			command: "grep -nE '^(func|type|var)' internal/shell/sentinel.go",
			wantID:  "grep-source-file-suggests-symbols",
			wantSub: "yc symbols internal/shell/sentinel.go",
		},
		{
			name:    "grep simple source file",
			command: "grep TODO foo.go",
			wantID:  "grep-source-file-suggests-symbols",
			wantSub: "yc symbols foo.go",
		},
		{
			name:    "curl URL captured",
			command: "curl -sL https://example.com/path",
			wantID:  "curl-http-suggests-browser",
			wantSub: "yc browser fetch https://example.com/path",
		},
		{
			name:    "wget URL captured",
			command: "wget https://example.com/x.tar.gz",
			wantID:  "wget-suggests-browser",
			wantSub: "yc browser fetch https://example.com/x.tar.gz",
		},
		{
			name:    "wc -l on source file",
			command: "wc -l internal/shell/sentinel.go",
			wantID:  "wc-source-suggests-symbols",
			wantSub: "yc symbols internal/shell/sentinel.go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetSeen()
			hints := Suggest(nil, tt.command)
			var msg string
			for _, h := range hints {
				if h.ID == tt.wantID {
					msg = h.Message
					break
				}
			}
			if msg == "" {
				t.Fatalf("rule %q did not fire on %q; got hints=%+v", tt.wantID, tt.command, hints)
			}
			if !strings.Contains(msg, tt.wantSub) {
				t.Fatalf("rule %q rendered %q; want substring %q", tt.wantID, msg, tt.wantSub)
			}
			if strings.Contains(msg, "$1") || strings.Contains(msg, "$2") {
				t.Fatalf("rule %q leaked unexpanded placeholder: %q", tt.wantID, msg)
			}
		})
	}
}

// TestSuggestStaticUnchanged confirms rules without capture groups still
// emit their literal Suggest text (ExpandString is a no-op for them).
func TestSuggestStaticUnchanged(t *testing.T) {
	ResetSeen()
	hints := Suggest(nil, "tree -L 2")
	if len(hints) == 0 {
		t.Fatalf("tree command produced no hints")
	}
	for _, h := range hints {
		if h.ID == "tree-suggests-repomap-or-graph" {
			if !strings.Contains(h.Message, "yc repomap") {
				t.Fatalf("tree hint missing expected text: %q", h.Message)
			}
			return
		}
	}
	t.Fatalf("tree hint did not fire; got %+v", hints)
}

// TestSuggestEmitsMetrics confirms each fired hint reaches the metrics
// sink with the expected (id, category, phase) tuple.
func TestSuggestEmitsMetrics(t *testing.T) {
	sink := installFakeMetrics(t)

	ResetSeen()
	_ = Suggest(nil, "git status")
	ResetSeen()
	_ = Suggest(nil, "grep -nE '^func' foo.go")
	ResetSeen()
	_ = SuggestPost(nil, 127, "")

	want := []hintObs{
		{"git-log-status-diff-suggests-yc-git", "git", "pre"},
		{"grep-source-file-suggests-symbols", "code-search", "pre"},
		{"exit-127-suggests-yc-help", "discovery", "post"},
	}
	if len(sink.hints) != len(want) {
		t.Fatalf("hint observations: want %d, got %d (%+v)", len(want), len(sink.hints), sink.hints)
	}
	for i, w := range want {
		if sink.hints[i] != w {
			t.Errorf("[%d] want %+v, got %+v", i, w, sink.hints[i])
		}
	}
}

// TestSuggestNoFalseFire keeps a small negative set so future regex
// changes don't start firing on benign input.
func TestSuggestNoFalseFire(t *testing.T) {
	negatives := []string{
		"echo hello world",
		"grep TODO README.md",
		"curl --version",
		"git --version",
	}
	for _, cmd := range negatives {
		t.Run(cmd, func(t *testing.T) {
			ResetSeen()
			hints := Suggest(nil, cmd)
			if len(hints) != 0 {
				t.Fatalf("command %q unexpectedly fired hints: %+v", cmd, hints)
			}
		})
	}
}
