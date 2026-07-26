package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The generated table must never advertise a hidden command. This is the
// failure the generator exists to prevent: the hand-written table it replaced
// documented most of the stubs as working features.
func TestRenderMarkdownOmitsHiddenCommands(t *testing.T) {
	r := newTestRegistry(t)
	md := RenderMarkdown(r)

	for name := range hiddenCommands {
		if strings.Contains(md, "`/"+name) {
			t.Errorf("generated docs advertise /%s, which is hidden", name)
		}
	}
	for _, spec := range r.List() {
		if !strings.Contains(md, "/"+spec.Name) {
			t.Errorf("generated docs omit the visible command /%s", spec.Name)
		}
	}
}

// A pipe is the cell separator; unescaped it silently splits a row into extra
// columns, which renders as a broken table rather than an error.
func TestRenderMarkdownEscapesPipes(t *testing.T) {
	r := NewRegistry()
	r.Register(&Spec{
		Name:        "x",
		Usage:       "/x [a|b]",
		Description: "does a|b",
		Category:    "session",
		Handler:     func(context.Context, string) (string, error) { return "", nil },
	})
	md := RenderMarkdown(r)
	if strings.Contains(md, "[a|b]") || strings.Contains(md, "does a|b") {
		t.Errorf("pipes were not escaped:\n%s", md)
	}
}

func TestReplaceDocSectionIsIdempotentAndNeedsMarkers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")

	// Missing markers must be an error, not a silent append that duplicates
	// the section on every run.
	if err := os.WriteFile(path, []byte("# Doc\n\nno markers here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplaceDocSection(path, "section"); err == nil {
		t.Error("expected an error when the markers are absent")
	}

	body := "# Doc\n\n" + DocBeginMarker + "\nold\n" + DocEndMarker + "\n\ntail\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	section := DocBeginMarker + "\nnew\n" + DocEndMarker + "\n"

	changed, err := ReplaceDocSection(path, section)
	if err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v", changed, err)
	}
	changed, err = ReplaceDocSection(path, section)
	if err != nil || changed {
		t.Fatalf("second write should be a no-op: changed=%v err=%v", changed, err)
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "tail") {
		t.Error("content outside the markers was lost")
	}
	if strings.Count(string(got), DocBeginMarker) != 1 {
		t.Error("the section was duplicated")
	}
}
