package detector

import "testing"

// TestClassifier_GoldenTable pins the qualify/disqualify decision per
// representative error message. The disqualify cases are the
// non-negotiable ones: anything matching them MUST stay quiet, even
// when later phases extend the qualify rules. The recent
// "exit status N from carrier program" case (commit 2ba0813) is the
// canonical regression risk.
func TestClassifier_GoldenTable(t *testing.T) {
	cases := []struct {
		name      string
		tool      string
		err       string
		want      Category
		qualifies bool
	}{
		// --- Must NOT fire (disqualify) ---
		{"carrier program exit 1 (lsof no match)", "bash", "exit status 1", "", false},
		{"carrier program exit 2 (grep)", "bash", "exit status 2", "", false},
		{"context deadline", "browser_navigate", "context deadline exceeded after 30s", "", false},
		{"connection refused", "promql_query", "Get http://localhost:9090/api/v1/query: dial tcp: connection refused", "", false},
		{"i/o timeout", "browser_lighthouse", "Get https://example.com: read tcp 10.0.0.1:80: i/o timeout", "", false},
		{"no such host", "browser_navigate", "no such host", "", false},
		{"signal: interrupt", "bash", "signal: interrupt", "", false},
		{"signal: killed", "bash", "signal: killed", "", false},
		{"permission denied", "Write", "open /etc/passwd: permission denied", "", false},
		{"bad user json", "Edit", "json: cannot unmarshal string into Go struct field of type int", "", false},
		{"bad user regex", "Grep", "invalid regex: error parsing regexp: missing closing ]: `[abc`", "", false},
		{"missing user file", "Read", "/some/path: file does not exist", "", false},

		// --- Must fire: broken ---
		{"panic", "browser_click", "panic: runtime error: index out of range", CategoryBroken, true},
		{"nil pointer", "loom_lease", "runtime error: invalid memory address or nil pointer dereference", CategoryBroken, true},
		{"unknown method", "anything", "mcp: unknown method: tools/whatever", CategoryBroken, true},
		{"unknown tool", "compositehandler", "unknown tool: foo_bar", CategoryBroken, true},
		{"marshal unsupported type", "browser_screenshot", "json: marshal: unsupported type chan int", CategoryBroken, true},
		{"-32601", "mcp_bridge", "JSON-RPC error -32601 method not found", CategoryBroken, true},
		{"-32602", "mcp_bridge", "JSON-RPC error -32602 invalid params", CategoryBroken, true},
		{"schema validation", "tool_dispatch", "schema validation failed: required field 'url' missing", CategoryBroken, true},

		// --- Must fire: missing ---
		{"not implemented", "browser_perf_start", "feature not implemented in live mode", CategoryMissing, true},
		{"not yet implemented", "loom_merge", "merge: not yet implemented for external backends", CategoryMissing, true},
		{"action not supported (live eval pre-a8a74f3)", "browser_eval", `live: action "evaluate" not supported`, CategoryMissing, true},
		{"returns an unsupported", "browser_eval", "live mode returns an unsupported error", CategoryMissing, true},
		{"not supported", "browser_perf_start", "perf_start: not supported in live mode", CategoryMissing, true},

		// --- Edge: empty error ---
		{"empty error", "browser_navigate", "", "", false},
	}
	c := &Classifier{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat, _, sig, q := c.Qualify(tc.tool, "ycode.tool.call", tc.err)
			if q != tc.qualifies {
				t.Fatalf("qualifies = %v; want %v\n  cat=%q sig=%q err=%q", q, tc.qualifies, cat, sig, tc.err)
			}
			if tc.qualifies {
				if cat != tc.want {
					t.Fatalf("category = %q; want %q (err=%q)", cat, tc.want, tc.err)
				}
				if sig == "" {
					t.Fatalf("qualified but no signature; err=%q", tc.err)
				}
			}
		})
	}
}

// TestClassifier_SignatureStability — semantically-identical errors
// (different timestamps, paths, line numbers, ports) must collapse to
// the same signature. This is the load-bearing invariant for dedupe
// and for Phase 2's backlog-already-exists check.
func TestClassifier_SignatureStability(t *testing.T) {
	c := &Classifier{}
	cases := []struct {
		group string
		errs  []string
	}{
		{
			"path normalization",
			[]string{
				"panic: open /Users/alice/proj/foo.go: file exists",
				"panic: open /Users/bob/different/path.go: file exists",
				"panic: open /home/carol/yet/another.go: file exists",
			},
		},
		{
			"line:col normalization",
			[]string{
				"unknown method tools/foo:123:45",
				"unknown method tools/foo:9999:1",
				"unknown method tools/foo:1:1",
			},
		},
		{
			"port normalization",
			[]string{
				"action not supported: localhost:8080 dial failed",
				"action not supported: localhost:65535 dial failed",
				"action not supported: 127.0.0.1:1234 dial failed",
			},
		},
		{
			"uuid normalization",
			[]string{
				"panic in session 11111111-2222-3333-4444-555555555555: nil",
				"panic in session ffffffff-eeee-dddd-cccc-bbbbbbbbbbbb: nil",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.group, func(t *testing.T) {
			var firstSig string
			for i, e := range tc.errs {
				_, _, sig, ok := c.Qualify("test_tool", "ycode.tool.call", e)
				if !ok {
					t.Fatalf("[%d] %q did not qualify; expected the group to be qualifying", i, e)
				}
				if i == 0 {
					firstSig = sig
					continue
				}
				if sig != firstSig {
					t.Fatalf("[%d] signature drift: %q != %q (err=%q)", i, sig, firstSig, e)
				}
			}
		})
	}
}

// TestClassifier_SignatureChangesAcrossTools — two different tools
// throwing the same error must NOT share a signature, since the fix
// would target different code paths.
func TestClassifier_SignatureChangesAcrossTools(t *testing.T) {
	c := &Classifier{}
	_, _, sigA, _ := c.Qualify("toolA", "ycode.tool.call", "panic: nil")
	_, _, sigB, _ := c.Qualify("toolB", "ycode.tool.call", "panic: nil")
	if sigA == sigB {
		t.Fatalf("tools toolA and toolB must hash to different signatures; both got %q", sigA)
	}
}
