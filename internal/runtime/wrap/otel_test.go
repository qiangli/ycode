package wrap

import (
	"context"
	"os"
	"testing"
)

func TestParseExportMode(t *testing.T) {
	cases := []struct {
		flag string
		env  string
		want ExportMode
	}{
		{"file", "", ExportFile},
		{"console", "", ExportConsole},
		{"off", "", ExportOff},
		{"", "", ExportFile},       // empty flag → default
		{"junk", "", ExportFile},   // unknown → default with warn
		{"file", "off", ExportOff}, // env wins
		{"off", "console", ExportConsole},
		{"file", "", ExportFile},
	}
	for _, tc := range cases {
		// Each subcase clears + maybe sets the env so the flag-wins
		// path stays isolated from external operator config.
		t.Run(tc.flag+"/"+tc.env, func(t *testing.T) {
			if tc.env == "" {
				_ = os.Unsetenv("YCODE_WRAP_OTEL_EXPORT")
			} else {
				t.Setenv("YCODE_WRAP_OTEL_EXPORT", tc.env)
			}
			got := ParseExportMode(tc.flag)
			if got != tc.want {
				t.Errorf("flag=%q env=%q: got %q, want %q", tc.flag, tc.env, got, tc.want)
			}
		})
	}
}

func TestSetupOTel_OffIsNoop(t *testing.T) {
	// ExportOff must return a callable shutdown closure without
	// installing any provider — required so the wrap CLI flag can
	// disable telemetry cleanly without other knock-on effects.
	shutdown := setupOTel(context.Background(), ExportOff, "claude", "claude")
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown closure")
	}
	shutdown() // must not panic
}

func TestSetupOTel_FileWritesUnderTempHome(t *testing.T) {
	// Redirect HOME so the wrap's per-instance data dir lands inside
	// t.TempDir(). The test doesn't inspect the file contents — the
	// SDK's batch span processor flushes asynchronously — it only
	// asserts that the instance dir was created and shutdown is
	// callable without error.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	shutdown := setupOTel(context.Background(), ExportFile, "claude", "claude")
	defer shutdown()

	dir := tmpHome + "/.agents/ycode/otel/instances"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read instances dir: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("expected at least one wrap-* instance dir under %s", dir)
	}
	foundWrap := false
	for _, e := range entries {
		if len(e.Name()) > 5 && e.Name()[:5] == "wrap-" {
			foundWrap = true
			break
		}
	}
	if !foundWrap {
		t.Errorf("no wrap-*-prefixed instance dir in %v", entries)
	}
}
