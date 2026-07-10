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

// TestSuggestCdBackgroundPrecedence pins the operator-precedence
// hint: `cd X && a & b` fires (because b runs in the parent cwd),
// while `(cd X && a &) && b` does NOT (parens scope the cd to the
// background subshell deliberately).
func TestSuggestCdBackgroundPrecedence(t *testing.T) {
	cases := []struct {
		name      string
		command   string
		wantFire  bool
		wantSubID string
	}{
		{
			name:      "cd then nohup then write pid file",
			command:   `cd /repo && build && nohup ./srv >log 2>&1 & echo $! > bin/.pid && tail log`,
			wantFire:  true,
			wantSubID: "cd-with-background-warns-precedence",
		},
		{
			name:     "cd then build && tail (no backgrounding)",
			command:  `cd /repo && make build && tail bin/foo.log`,
			wantFire: false,
		},
		{
			name:     "background without a cd (no cwd surprise)",
			command:  `nohup ./srv >log 2>&1 & echo $! > /tmp/pid`,
			wantFire: false,
		},
		{
			name:     "parens scope the backgrounded segment to its own subshell",
			command:  `(cd /repo && nohup ./srv >log 2>&1 &) && tail /tmp/log`,
			wantFire: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ResetSeen()
			hints := Suggest(nil, tc.command)
			var fired bool
			for _, h := range hints {
				if h.ID == "cd-with-background-warns-precedence" {
					fired = true
					if !strings.Contains(h.Message, "operator precedence") {
						t.Fatalf("hint message should explain operator precedence: %q", h.Message)
					}
				}
			}
			if fired != tc.wantFire {
				t.Fatalf("command %q: wantFire=%v, gotFired=%v (hints=%+v)", tc.command, tc.wantFire, fired, hints)
			}
		})
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

// TestSuggestSkipsRemoteSSH pins that ssh-as-remote-exec suppresses all
// catalog hints — the body runs on another host, so suggestions like
// `yc symbols` against the local tree would be misleading.
func TestSuggestSkipsRemoteSSH(t *testing.T) {
	remotes := []string{
		`ssh user@host 'echo foo | grep bar'`,
		`/usr/bin/env ssh -o ConnectTimeout=10 -o BatchMode=yes alice@host-b.local 'echo "== mem ==" && grep -r MemTotal /proc/meminfo'`,
		`env LANG=C ssh host 'tree -L 2'`,
		`nohup ssh host 'curl https://example.com'`,
		`PATH=/usr/bin ssh host 'wget https://example.com'`,
	}
	for _, cmd := range remotes {
		t.Run(cmd, func(t *testing.T) {
			ResetSeen()
			hints := Suggest(nil, cmd)
			if len(hints) != 0 {
				t.Fatalf("ssh remote-exec command unexpectedly fired hints: %+v", hints)
			}
		})
	}
}

// TestSuggestEmitsWhy confirms every hint that fires carries a non-empty
// Why rationale — the field exists to overcome muscle memory, so a hint
// without one is a regression.
func TestSuggestEmitsWhy(t *testing.T) {
	ResetSeen()
	hints := Suggest(nil, "grep -rn FuncName .")
	if len(hints) == 0 {
		t.Fatalf("expected at least one hint for `grep -rn FuncName .`")
	}
	for _, h := range hints {
		if h.Why == "" {
			t.Errorf("hint %q has empty Why; every catalog entry must supply one", h.ID)
		}
	}
}

// TestSessionDedupAcrossProcesses pins that the file-backed dedup keeps
// state across two ResetSeen calls when $YCODE_SESSION_ID is set — the
// proxy for the multi-process case where each `ycode shell -c` is fresh
// but the session ID is shared.
func TestSessionDedupAcrossProcesses(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)
	t.Setenv("YCODE_SESSION_ID", "test-session-"+t.Name())

	ResetSeen()
	first := Suggest(nil, "grep -rn foo .")
	if len(first) == 0 {
		t.Fatalf("first pass produced no hints")
	}

	// Simulate a fresh process: clear the in-memory map but keep the
	// session env vars + file. The lazy loader should repopulate from disk.
	ResetSeen()
	second := Suggest(nil, "grep -rn foo .")
	if len(second) != 0 {
		t.Fatalf("session-level dedup failed: second pass re-fired %d hints (%+v)", len(second), second)
	}
}

// TestSessionDedupOptOut confirms that without $YCODE_SESSION_ID we
// fall back to process-local behavior — ResetSeen wipes state, and the
// next call re-fires.
func TestSessionDedupOptOut(t *testing.T) {
	t.Setenv("YCODE_SESSION_ID", "")

	ResetSeen()
	first := Suggest(nil, "grep -rn foo .")
	if len(first) == 0 {
		t.Fatalf("first pass produced no hints")
	}
	ResetSeen()
	second := Suggest(nil, "grep -rn foo .")
	if len(second) == 0 {
		t.Fatalf("without YCODE_SESSION_ID, dedup must reset with the in-memory map")
	}
}

// TestGrepRecursiveMessageNamesMatchedFlag pins the agent-feedback fix:
// the grep -r hint must name the matched flag in its rendered message
// (e.g. "noticing `-rn` on grep — try yc search-symbols ..."). Otherwise
// agents see the generic "try yc" suggestion, can't tell why it fired,
// and end up grepping their command for unrelated flags trying to
// understand which token tripped the wrapper.
func TestGrepRecursiveMessageNamesMatchedFlag(t *testing.T) {
	ResetSeen()
	hints := Suggest(nil, "grep -rn FuncName .")
	var msg string
	for _, h := range hints {
		if h.ID == "grep-r-suggests-search-symbols" {
			msg = h.Message
			break
		}
	}
	if msg == "" {
		t.Fatalf("grep-r hint did not fire; got %+v", hints)
	}
	if !strings.Contains(msg, "-rn") {
		t.Fatalf("expected matched-flag callout in message; got %q", msg)
	}
}

// TestSkipOnSuccessPropagates pins that pre-exec hints that opt into
// SkipOnSuccess on the catalog side surface that flag on the shell.Hint
// the caller sees — otherwise the dispatcher can't suppress them on
// exit 0 and the retro-feedback noise complaint reproduces.
func TestSkipOnSuccessPropagates(t *testing.T) {
	ResetSeen()
	hints := Suggest(nil, "git status")
	if len(hints) == 0 {
		t.Fatalf("git hint did not fire")
	}
	for _, h := range hints {
		if h.ID == "git-log-status-diff-suggests-yc-git" && !h.SkipOnSuccess {
			t.Fatalf("git hint must carry SkipOnSuccess so exit-0 idiomatic invocations don't repeat-pitch yc")
		}
	}
}

// TestBinPathPostHintRewritesPerTool covers the /bin/<tool> ENOENT
// detector: when stderr names a /bin path that doesn't exist on macOS,
// the hint must name BOTH the /usr/bin/ fix AND a ycode-native verb to
// upgrade toward. Failures are the right moment to upsell yc — exit-0
// commands shouldn't get this pitch.
func TestBinPathPostHintRewritesPerTool(t *testing.T) {
	cases := []struct {
		name    string
		stderr  string
		wantBin string // /usr/bin/<X> the user should swap to
		wantYC  string // ycode-native verb the hint should namedrop
	}{
		{
			name:    "/bin/grep ENOENT names search-symbols",
			stderr:  "/bin/sh: /bin/grep: No such file or directory\n",
			wantBin: "/usr/bin/grep",
			wantYC:  "yc search-symbols",
		},
		{
			name:    "/bin/sed ENOENT names yc git",
			stderr:  "/bin/sed: not found\n",
			wantBin: "/usr/bin/sed",
			wantYC:  "yc git",
		},
		{
			name:    "/bin/awk ENOENT names bash arithmetic",
			stderr:  "exec: /bin/awk: no such file or directory\n",
			wantBin: "/usr/bin/awk",
			wantYC:  "bash arithmetic",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ResetSeen()
			hints := SuggestPost(nil, 127, tc.stderr)
			var msg string
			for _, h := range hints {
				if h.ID == "bin-path-suggests-usr-bin-and-yc" {
					msg = h.Message
					break
				}
			}
			if msg == "" {
				t.Fatalf("bin-path hint did not fire on %q; got %+v", tc.stderr, hints)
			}
			if !strings.Contains(msg, tc.wantBin) {
				t.Fatalf("expected %q in message %q", tc.wantBin, msg)
			}
			if !strings.Contains(msg, tc.wantYC) {
				t.Fatalf("expected ycode-feature %q in message %q", tc.wantYC, msg)
			}
		})
	}
}

// TestBinPathPostHintIgnoresClean confirms the bin-path detector doesn't
// fire on stderr without a /bin/X reference, or on a clean exit.
func TestBinPathPostHintIgnoresClean(t *testing.T) {
	ResetSeen()
	hints := SuggestPost(nil, 0, "")
	for _, h := range hints {
		if h.ID == "bin-path-suggests-usr-bin-and-yc" {
			t.Fatalf("bin-path hint fired on clean exit + empty stderr")
		}
	}
	ResetSeen()
	hints = SuggestPost(nil, 1, "permission denied\n")
	for _, h := range hints {
		if h.ID == "bin-path-suggests-usr-bin-and-yc" {
			t.Fatalf("bin-path hint fired on unrelated permission-denied stderr")
		}
	}
}

// TestIsRemoteExec covers the wrapper-stripping logic so regressions in
// argument parsing surface independently of the catalog.
func TestIsRemoteExec(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"ssh host echo hi", true},
		{"ssh user@host 'echo hi'", true},
		{"/usr/bin/env ssh -o BatchMode=yes user@host date", true},
		{"env FOO=bar ssh host date", true},
		{"FOO=bar BAZ=qux ssh host date", true},
		{"nohup ssh host date", true},
		{"time ssh host date", true},
		{"ssh-keygen -t ed25519", false},
		{"ssh-add -l", false},
		{"echo ssh user@host", false},
		{"git log", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isRemoteExec(tc.cmd); got != tc.want {
			t.Errorf("isRemoteExec(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}
