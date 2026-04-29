//go:build !cgo

package treesitter

import (
	"context"
	"errors"
	"testing"
)

func TestNoCGO_NewParser(t *testing.T) {
	p := NewParser()
	if p == nil {
		t.Fatal("NewParser should return non-nil even without CGO")
	}
}

func TestNoCGO_ParseReturnsError(t *testing.T) {
	p := NewParser()
	_, err := p.Parse(context.Background(), []byte("package main"), "go")
	if err == nil {
		t.Fatal("Parse should return error without CGO")
	}
	if !errors.Is(err, ErrNoCGO) {
		t.Errorf("expected ErrNoCGO, got: %v", err)
	}
}

func TestNoCGO_IsSupported(t *testing.T) {
	languages := []string{"go", "python", "javascript", "typescript", "rust", "java", "c", "ruby"}
	for _, lang := range languages {
		if IsSupported(lang) {
			t.Errorf("IsSupported(%q) should be false without CGO", lang)
		}
	}
}

func TestNoCGO_SupportedLanguages(t *testing.T) {
	langs := SupportedLanguages()
	if len(langs) != 0 {
		t.Errorf("SupportedLanguages should return empty slice without CGO, got %d", len(langs))
	}
}

func TestNoCGO_GetLanguage(t *testing.T) {
	if GetLanguage("go") != nil {
		t.Error("GetLanguage should return nil without CGO")
	}
}

func TestNoCGO_Search(t *testing.T) {
	p := NewParser()
	_, err := Search(context.Background(), p, []byte("package main"), "go", "(function_declaration)", "main.go")
	if err == nil {
		t.Fatal("Search should return error without CGO")
	}
	if !errors.Is(err, ErrNoCGO) {
		t.Errorf("expected ErrNoCGO, got: %v", err)
	}
}

func TestNoCGO_SearchText(t *testing.T) {
	p := NewParser()
	_, err := SearchText(context.Background(), p, []byte("package main"), "go", "func main", "main.go")
	if err == nil {
		t.Fatal("SearchText should return error without CGO")
	}
	if !errors.Is(err, ErrNoCGO) {
		t.Errorf("expected ErrNoCGO, got: %v", err)
	}
}

func TestNoCGO_Analyze(t *testing.T) {
	p := NewParser()
	_, err := Analyze(context.Background(), p, "Foo", "main.go", "/tmp")
	if err == nil {
		t.Fatal("Analyze should return error without CGO")
	}
	if !errors.Is(err, ErrNoCGO) {
		t.Errorf("expected ErrNoCGO, got: %v", err)
	}
}

func TestNoCGO_ExtractSymbols(t *testing.T) {
	symbols := ExtractSymbols(nil, "main.go")
	if symbols != nil {
		t.Error("ExtractSymbols should return nil without CGO")
	}
}

func TestNoCGO_ErrNoCGO(t *testing.T) {
	if ErrNoCGO == nil {
		t.Fatal("ErrNoCGO should not be nil")
	}
	if ErrNoCGO.Error() == "" {
		t.Error("ErrNoCGO should have a message")
	}
}
