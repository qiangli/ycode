//go:build !windows

package wrap

import (
	"bytes"
	"testing"
)

func TestParsePTYMode(t *testing.T) {
	cases := []struct {
		flag string
		want PTYMode
	}{
		{"auto", PTYAuto},
		{"always", PTYAlways},
		{"never", PTYNever},
		{"", PTYAuto},
		{"junk", PTYAuto},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			got := ParsePTYMode(tc.flag)
			if got != tc.want {
				t.Errorf("flag=%q: got %q, want %q", tc.flag, got, tc.want)
			}
		})
	}
}

func TestShouldAllocatePTY_ModeOverrides(t *testing.T) {
	// always → true regardless of stdio
	if !shouldAllocatePTY(PTYAlways, Options{}) {
		t.Errorf("always must allocate even without TTY")
	}
	// never → false regardless of stdio
	if shouldAllocatePTY(PTYNever, Options{}) {
		t.Errorf("never must not allocate")
	}
}

func TestShouldAllocatePTY_AutoWithBufferStdio(t *testing.T) {
	// Tests provide *bytes.Buffer for Stdin/Stdout — those are NOT
	// terminals, so auto must return false. This is the regression
	// gate for the e2e tests that inject byte buffers.
	opts := Options{
		Stdin:  &bytes.Buffer{},
		Stdout: &bytes.Buffer{},
	}
	if shouldAllocatePTY(PTYAuto, opts) {
		t.Errorf("auto must not allocate with non-file stdio")
	}
}
