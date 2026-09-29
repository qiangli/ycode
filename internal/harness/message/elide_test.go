package message

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestElideMiddleKeepsHeadTailAndCountsTheGap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 1000; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", 20))
		b.WriteString("\n")
	}
	text := "FIRST\n" + b.String() + "LAST\n"
	got, elided := ElideMiddle(text, 1000)
	if !elided {
		t.Fatal("text over the bound must be elided")
	}
	if len(got) > 1100 {
		t.Fatalf("bounded text = %d bytes, want about 1000", len(got))
	}
	if !strings.HasPrefix(got, "FIRST\n") || !strings.HasSuffix(got, "LAST\n") {
		t.Fatalf("head/tail lost: %.40q ... %.40q", got, got[len(got)-40:])
	}
	note := got[strings.Index(got, "[output truncated: "):]
	note = note[:strings.Index(note, "]")+1]
	keptLines := strings.Count(got, "\n") - 1 // the note line adds one newline
	var lines, bytes int
	if _, err := fmtSscanf(note, &lines, &bytes); err != nil {
		t.Fatalf("note %q: %v", note, err)
	}
	if lines+keptLines != strings.Count(text, "\n") {
		t.Fatalf("note %q: kept %d + omitted %d lines != %d", note, keptLines, lines, strings.Count(text, "\n"))
	}
	if bytes <= 0 || bytes >= len(text) {
		t.Fatalf("note %q: omitted bytes out of range", note)
	}
}

func TestElideMiddleLeavesShortTextAndRunes(t *testing.T) {
	if got, elided := ElideMiddle("short", 100); elided || got != "short" {
		t.Fatalf("short text changed: %q", got)
	}
	if got, elided := ElideMiddle("anything", 0); elided || got != "anything" {
		t.Fatalf("maxBytes 0 must disable the bound: %q", got)
	}
	got, _ := ElideMiddle(strings.Repeat("é", 500), 101)
	if !utf8.ValidString(got) {
		t.Fatalf("elision split a rune: %q", got)
	}
}

func fmtSscanf(note string, lines, bytes *int) (int, error) {
	return fmt.Sscanf(note, "[output truncated: %d lines (%d bytes) omitted]", lines, bytes)
}
