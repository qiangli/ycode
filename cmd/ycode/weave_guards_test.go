package main

import (
	"os"
	"testing"
)

func TestParseWeaveMemLimit(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"16g", 16 << 30, false},
		{"16G", 16 << 30, false},
		{"16gb", 16 << 30, false},
		{"512m", 512 << 20, false},
		{"1024k", 1024 << 10, false},
		{"123", 123, false},
		{" 2g ", 2 << 30, false},
		{"-1g", 0, true},
		{"abc", 0, true},
		{"g", 0, true},
	}
	for _, c := range cases {
		got, err := parseWeaveMemLimit(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseWeaveMemLimit(%q): expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseWeaveMemLimit(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseWeaveMemLimit(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTailOffset(t *testing.T) {
	write := func(content string) *os.File {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), "tail")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(content); err != nil {
			t.Fatal(err)
		}
		return f
	}
	cases := []struct {
		name    string
		content string
		n       int
		want    string // expected substring from offset to EOF
	}{
		{"whole file when n exceeds lines", "a\nb\nc\n", 10, "a\nb\nc\n"},
		{"last two lines", "a\nb\nc\n", 2, "b\nc\n"},
		{"last line with trailing newline", "a\nb\nc\n", 1, "c\n"},
		{"last line without trailing newline", "a\nb\nc", 1, "c"},
		{"zero lines returns end", "a\nb\n", 0, ""},
		{"empty file", "", 3, ""},
		{"single line no newline", "abc", 1, "abc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := write(c.content)
			defer f.Close()
			off, err := tailOffset(f, c.n)
			if err != nil {
				t.Fatalf("tailOffset: %v", err)
			}
			got := c.content[off:]
			if got != c.want {
				t.Errorf("tailOffset(%q, %d): got %q, want %q", c.content, c.n, got, c.want)
			}
		})
	}
}
