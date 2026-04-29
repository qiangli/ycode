//go:build cgo

package treesitter

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
)

// Tree represents a parsed source file (CGO build).
type Tree struct {
	Root   *sitter.Node
	Source []byte
	Lang   string
}

// Parser wraps tree-sitter parsing for multiple languages (CGO build).
type Parser struct {
	parser *sitter.Parser
}

// NewParser creates a new tree-sitter parser.
func NewParser() *Parser {
	return &Parser{
		parser: sitter.NewParser(),
	}
}

// Parse parses source code in the given language and returns the AST.
func (p *Parser) Parse(ctx context.Context, source []byte, lang string) (*Tree, error) {
	language := GetLanguage(lang)
	if language == nil {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	p.parser.SetLanguage(language)
	tree, err := p.parser.ParseCtx(ctx, nil, source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", lang, err)
	}

	return &Tree{
		Root:   tree.RootNode(),
		Source: source,
		Lang:   lang,
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
func WalkNodes(node *sitter.Node, fn func(*sitter.Node) bool) {
	if node == nil {
		return
	}
	if !fn(node) {
		return
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		WalkNodes(node.Child(i), fn)
	}
}

// nodeText extracts the source text for a node.
func nodeText(node *sitter.Node, source []byte) string {
	return node.Content(source)
}

// --- Language-specific symbol extractors ---

func extractGoSymbols(tree *Tree, file string) []Symbol {
	var symbols []Symbol
	root := tree.Root

	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "function_declaration":
			name := findChild(child, "name")
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
			name := findChild(child, "name")
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
			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(j)
				if spec.Type() == "type_spec" {
					name := findChild(spec, "name")
					if name != nil {
						kind := "type"
						typeNode := findChild(spec, "type")
						if typeNode != nil && typeNode.Type() == "interface_type" {
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

	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "function_definition":
			name := findChild(child, "name")
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
			name := findChild(child, "name")
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
	WalkNodes(tree.Root, func(node *sitter.Node) bool {
		switch node.Type() {
		case "function_declaration":
			name := findChild(node, "name")
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
			name := findChild(node, "name")
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

	// Also look for TypeScript-specific declarations.
	WalkNodes(tree.Root, func(node *sitter.Node) bool {
		switch node.Type() {
		case "interface_declaration":
			name := findChild(node, "name")
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
			name := findChild(node, "name")
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

	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "function_item":
			name := findChild(child, "name")
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "func",
					Signature: "fn " + n,
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  hasVisibilityModifier(child, tree.Source),
				})
			}
		case "struct_item":
			name := findChild(child, "name")
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "type",
					Signature: "struct " + n,
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  hasVisibilityModifier(child, tree.Source),
				})
			}
		case "enum_item":
			name := findChild(child, "name")
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "type",
					Signature: "enum " + n,
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  hasVisibilityModifier(child, tree.Source),
				})
			}
		case "trait_item":
			name := findChild(child, "name")
			if name != nil {
				n := nodeText(name, tree.Source)
				symbols = append(symbols, Symbol{
					Name:      n,
					Kind:      "interface",
					Signature: "trait " + n,
					File:      file,
					Line:      int(child.StartPoint().Row) + 1,
					Exported:  hasVisibilityModifier(child, tree.Source),
				})
			}
		case "impl_item":
			name := findChild(child, "type")
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
	WalkNodes(tree.Root, func(node *sitter.Node) bool {
		switch node.Type() {
		case "class_declaration":
			name := findChild(node, "name")
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
			name := findChild(node, "name")
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
			name := findChild(node, "name")
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
	WalkNodes(tree.Root, func(node *sitter.Node) bool {
		switch node.Type() {
		case "method":
			name := findChild(node, "name")
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
			name := findChild(node, "name")
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
			name := findChild(node, "name")
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

func findChild(node *sitter.Node, fieldName string) *sitter.Node {
	return node.ChildByFieldName(fieldName)
}

func isUpperFirst(s string) bool {
	if s == "" {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

func startsWithUnderscore(s string) bool {
	return len(s) > 0 && s[0] == '_'
}

func hasVisibilityModifier(node *sitter.Node, source []byte) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "visibility_modifier" {
			return true
		}
	}
	return false
}
