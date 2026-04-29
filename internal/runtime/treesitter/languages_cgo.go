//go:build cgo

package treesitter

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// langRegistry maps language names to tree-sitter grammar constructors.
var langRegistry = map[string]*sitter.Language{
	"go":         golang.GetLanguage(),
	"python":     python.GetLanguage(),
	"javascript": javascript.GetLanguage(),
	"typescript": typescript.GetLanguage(),
	"tsx":        tsx.GetLanguage(),
	"rust":       rust.GetLanguage(),
	"java":       java.GetLanguage(),
	"c":          c.GetLanguage(),
	"ruby":       ruby.GetLanguage(),
}

// langAliases maps file extension aliases to canonical language names.
var langAliases = map[string]string{
	"go":   "go",
	"py":   "python",
	"js":   "javascript",
	"jsx":  "javascript",
	"ts":   "typescript",
	"tsx":  "tsx",
	"rs":   "rust",
	"java": "java",
	"c":    "c",
	"h":    "c",
	"rb":   "ruby",
}

// GetLanguage returns the tree-sitter language for the given name or alias.
// Returns nil if the language is not supported.
func GetLanguage(name string) *sitter.Language {
	if lang, ok := langRegistry[name]; ok {
		return lang
	}
	if canonical, ok := langAliases[name]; ok {
		return langRegistry[canonical]
	}
	return nil
}

// SupportedLanguages returns all supported language names.
func SupportedLanguages() []string {
	langs := make([]string, 0, len(langRegistry))
	for name := range langRegistry {
		langs = append(langs, name)
	}
	return langs
}

// IsSupported returns true if the given language name or alias is supported.
func IsSupported(name string) bool {
	return GetLanguage(name) != nil
}
