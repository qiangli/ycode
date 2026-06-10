package main

import "testing"

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
