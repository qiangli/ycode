package treesitter

import (
	"context"
	"fmt"

	"github.com/odvcencio/gotreesitter"
)

// Tree represents a parsed source file.
type Tree struct {
	Root     *gotreesitter.Node
	Source   []byte
	Lang     string
	language *gotreesitter.Language // kept for node operations that require it
}

// Parser wraps tree-sitter parsing for multiple languages.
type Parser struct {
	parsers map[string]*gotreesitter.Parser
}

// NewParser creates a new tree-sitter parser.
func NewParser() *Parser {
	return &Parser{
		parsers: make(map[string]*gotreesitter.Parser),
	}
}

// getParser returns a cached parser for the given language, creating one if needed.
func (p *Parser) getParser(lang string, language *gotreesitter.Language) *gotreesitter.Parser {
	if parser, ok := p.parsers[lang]; ok {
		return parser
	}
	parser := gotreesitter.NewParser(language)
	p.parsers[lang] = parser
	return parser
}

// Parse parses source code in the given language and returns the AST.
func (p *Parser) Parse(_ context.Context, source []byte, lang string) (*Tree, error) {
	language := GetLanguage(lang)
	if language == nil {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	parser := p.getParser(lang, language)
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", lang, err)
	}

	return &Tree{
		Root:     tree.RootNode(),
		Source:   source,
		Lang:     lang,
		language: language,
	}, nil
}

// ExtractSymbols extracts top-level symbols from a parsed tree.
// This is a language-aware extraction that understands common patterns
// across supported languages.
func ExtractSymbols(tree *Tree, file string) []Symbol {
	if tree == nil || tree.Root == nil {
		return nil
	}

	switch tree.Lang {
	case "go":
		return extractGoSymbols(tree, file)
	case "python":
		return extractPythonSymbols(tree, file)
	case "javascript", "tsx":
		return extractJSSymbols(tree, file)
	case "typescript":
		return extractTSSymbols(tree, file)
	case "rust":
		return extractRustSymbols(tree, file)
	case "java":
		return extractJavaSymbols(tree, file)
	case "ruby":
		return extractRubySymbols(tree, file)
	default:
		return extractGenericSymbols(tree, file)
	}
}

// WalkNodes calls fn for every node in the tree (depth-first).
func WalkNodes(node *gotreesitter.Node, lang *gotreesitter.Language, fn func(*gotreesitter.Node) bool) {
	if node == nil {
		return
	}
	if !fn(node) {
		return
	}
	for i := 0; i < node.ChildCount(); i++ {
		WalkNodes(node.Child(i), lang, fn)
	}
}

// nodeText extracts the source text for a node.
func nodeText(node *gotreesitter.Node, source []byte) string {
	return node.Text(source)
}

// --- Language-specific symbol extractors ---

func extractGoSymbols(tree *Tree, file string) []Symbol {
	var symbols []Symbol
	root := tree.Root
	lang := tree.language

	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Type(lang) {
		case "function_declaration":
			name := child.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "func",
					Signature: "func " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  isUpperFirst(nodeText(name, tree.Source)),
				})
			}
		case "method_declaration":
			name := child.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "method",
					Signature: "method " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  isUpperFirst(nodeText(name, tree.Source)),
				})
			}
		case "type_declaration":
			for j := 0; j < child.ChildCount(); j++ {
				spec := child.Child(j)
				if spec.Type(lang) == "type_spec" {
					name := spec.ChildByFieldName("name", lang)
					if name != nil {
						kind := "type"
						typeNode := spec.ChildByFieldName("type", lang)
						if typeNode != nil && typeNode.Type(lang) == "interface_type" {
							kind = "interface"
						}
						symbols = append(symbols, Symbol{
							Name:      nodeText(name, tree.Source),
							Kind:      kind,
							Signature: kind + " " + nodeText(name, tree.Source),
							File:      file,
							Line:      int(spec.StartPoint().Row) + 1,
							Exported:  isUpperFirst(nodeText(name, tree.Source)),
						})
					}
				}
			}
		}
	}
	return symbols
}

func extractPythonSymbols(tree *Tree, file string) []Symbol {
	var symbols []Symbol
	root := tree.Root
	lang := tree.language

	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Type(lang) {
		case "function_definition":
			name := child.ChildByFieldName("name", lang)
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "func",
					Signature: "def " + n,
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  !startsWithUnderscore(n),
				})
			}
		case "class_definition":
			name := child.ChildByFieldName("name", lang)
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "class",
					Signature: "class " + n,
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  !startsWithUnderscore(n),
				})
			}
		}
	}
	return symbols
}

func extractJSSymbols(tree *Tree, file string) []Symbol {
	var symbols []Symbol
	lang := tree.language
	WalkNodes(tree.Root, lang, func(node *gotreesitter.Node) bool {
		switch node.Type(lang) {
		case "function_declaration":
			name := node.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "func",
					Signature: "function " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(node.StartPoint().Row) + 1,
					Exported:  true, // TODO: check export keyword
				})
			}
			return false
		case "class_declaration":
			name := node.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "class",
					Signature: "class " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(node.StartPoint().Row) + 1,
					Exported:  true,
				})
			}
			return false
		}
		return true
	})
	return symbols
}

func extractTSSymbols(tree *Tree, file string) []Symbol {
	symbols := extractJSSymbols(tree, file)
	lang := tree.language

	// Also look for TypeScript-specific declarations.
	WalkNodes(tree.Root, lang, func(node *gotreesitter.Node) bool {
		switch node.Type(lang) {
		case "interface_declaration":
			name := node.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "interface",
					Signature: "interface " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(node.StartPoint().Row) + 1,
					Exported:  true,
				})
			}
			return false
		case "type_alias_declaration":
			name := node.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "type",
					Signature: "type " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(node.StartPoint().Row) + 1,
					Exported:  true,
				})
			}
			return false
		}
		return true
	})
	return symbols
}

func extractRustSymbols(tree *Tree, file string) []Symbol {
	var symbols []Symbol
	root := tree.Root
	lang := tree.language

	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Type(lang) {
		case "function_item":
			name := child.ChildByFieldName("name", lang)
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "func",
					Signature: "fn " + n,
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  hasVisibilityModifier(child, lang),
				})
			}
		case "struct_item":
			name := child.ChildByFieldName("name", lang)
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "type",
					Signature: "struct " + n,
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  hasVisibilityModifier(child, lang),
				})
			}
		case "enum_item":
			name := child.ChildByFieldName("name", lang)
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "type",
					Signature: "enum " + n,
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  hasVisibilityModifier(child, lang),
				})
			}
		case "trait_item":
			name := child.ChildByFieldName("name", lang)
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "interface",
					Signature: "trait " + n,
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  hasVisibilityModifier(child, lang),
				})
			}
		case "impl_item":
			name := child.ChildByFieldName("type", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "type",
					Signature: "impl " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  true,
				})
			}
		}
	}
	return symbols
}

func extractJavaSymbols(tree *Tree, file string) []Symbol {
	var symbols []Symbol
	lang := tree.language
	WalkNodes(tree.Root, lang, func(node *gotreesitter.Node) bool {
		switch node.Type(lang) {
		case "class_declaration":
			name := node.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "class",
					Signature: "class " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(node.StartPoint().Row) + 1,
					Exported:  true,
				})
			}
			return false
		case "interface_declaration":
			name := node.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "interface",
					Signature: "interface " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(node.StartPoint().Row) + 1,
					Exported:  true,
				})
			}
			return false
		case "enum_declaration":
			name := node.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "type",
					Signature: "enum " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(node.StartPoint().Row) + 1,
					Exported:  true,
				})
			}
			return false
		}
		return true
	})
	return symbols
}

func extractRubySymbols(tree *Tree, file string) []Symbol {
	var symbols []Symbol
	lang := tree.language
	WalkNodes(tree.Root, lang, func(node *gotreesitter.Node) bool {
		switch node.Type(lang) {
		case "method":
			name := node.ChildByFieldName("name", lang)
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "func",
					Signature: "def " + n,
					File:      file,
					Line:      int(node.StartPoint().Row) + 1,
					Exported:  !startsWithUnderscore(n),
				})
			}
			return false
		case "class":
			name := node.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "class",
					Signature: "class " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(node.StartPoint().Row) + 1,
					Exported:  true,
				})
			}
			return false
		case "module":
			name := node.ChildByFieldName("name", lang)
			if name != nil {
				symbols = append(symbols, Symbol{
					Name:      nodeText(name, tree.Source),
					Kind:      "type",
					Signature: "module " + nodeText(name, tree.Source),
					File:      file,
					Line:      int(node.StartPoint().Row) + 1,
					Exported:  true,
				})
			}
			return false
		}
		return true
	})
	return symbols
}

func extractGenericSymbols(tree *Tree, file string) []Symbol {
	// Fallback: no language-specific extraction.
	return nil
}

// --- Helpers ---

func isUpperFirst(s string) bool {
	if s == "" {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

func startsWithUnderscore(s string) bool {
	return len(s) > 0 && s[0] == '_'
}

func hasVisibilityModifier(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Type(lang) == "visibility_modifier" {
			return true
		}
	}
	return false
}
