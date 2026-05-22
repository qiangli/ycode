package wrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadServeManifestAt(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		contents string
		wantAddr string
		wantOK   bool
	}{
		{
			name: "happy path with otlpGRPC populated",
			contents: `{
				"schemaVersion": "4",
				"endpoints": {
					"otlpGRPC": "127.0.0.1:4317",
					"otlpHTTP": "http://127.0.0.1:4318"
				}
			}`,
			wantAddr: "127.0.0.1:4317",
			wantOK:   true,
		},
		{
			name: "endpoints map present but otlpGRPC missing",
			contents: `{
				"endpoints": {"git": "http://127.0.0.1:31415/git/"}
			}`,
			wantOK: false,
		},
		{
			name:     "no endpoints key at all",
			contents: `{"schemaVersion": "4"}`,
			wantOK:   false,
		},
		{
			name:     "malformed json",
			contents: `{not json`,
			wantOK:   false,
		},
		{
			name:     "otlpGRPC is empty string",
			contents: `{"endpoints": {"otlpGRPC": ""}}`,
			wantOK:   false,
		},
		{
			name:     "otlpGRPC is not a string",
			contents: `{"endpoints": {"otlpGRPC": 4317}}`,
			wantOK:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "manifest.json")
			if err := os.WriteFile(path, []byte(tc.contents), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			gotAddr, gotOK := readServeManifestAt(path)
			if gotOK != tc.wantOK {
				t.Errorf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotAddr != tc.wantAddr {
				t.Errorf("addr = %q, want %q", gotAddr, tc.wantAddr)
			}
		})
	}
}

func TestReadServeManifestAt_MissingFile(t *testing.T) {
	addr, ok := readServeManifestAt(filepath.Join(t.TempDir(), "nope.json"))
	if ok {
		t.Errorf("ok = true for missing file (addr=%q)", addr)
	}
	if addr != "" {
		t.Errorf("addr = %q, want empty for missing file", addr)
	}
}
