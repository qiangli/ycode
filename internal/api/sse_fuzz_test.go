package api

import (
	"strings"
	"testing"
)

func FuzzSSEParser(f *testing.F) {
	f.Add("event: test\ndata: hello\n\n")
	f.Add("data: {\"key\":\"value\"}\n\n")
	f.Add(": comment\nevent: x\ndata: y\n\n")
	f.Add("event: a\ndata: line1\ndata: line2\n\n")
	f.Add("")
	f.Add("\n\n\n")

	f.Fuzz(func(t *testing.T, input string) {
		parser := NewSSEParser(strings.NewReader(input))
		for i := 0; i < 100; i++ {
			_, err := parser.Next()
			if err != nil {
				return
			}
		}
	})
}
