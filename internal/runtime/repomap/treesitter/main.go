// ts-symbols extracts top-level symbols from source files using tree-sitter.
//
// It reads a JSON manifest from stdin listing files to parse, and writes
// a JSON array of symbols to stdout. This program is designed to run
// inside a container so that the CGO/tree-sitter dependency is isolated
// from the ycode host binary.
//
// Input (stdin):  {"files": [{"path": "/workspace/src/foo.py", "rel": "src/foo.py", "lang": "python"}]}
// Output (stdout): [{"name": "Foo", "kind": "class", "signature": "class Foo", "file": "src/foo.py", "line": 10, "exported": true}]
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	tsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
)

type InputFile struct {
	Path string `json:"path"`
	Rel  string `json:"rel"`
	Lang string `json:"lang"`
}

type Input struct {
	Files []InputFile `json:"files"`
}

type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Signature string `json:"signature,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Exported  bool   `json:"exported"`
}

func main() {
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fmt.Fprintf(os.Stderr, "parse input: %v\n", err)
		os.Exit(1)
	}

	var allSymbols []Symbol
	for _, f := range input.Files {
		symbols, err := parseFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse %s: %v\n", f.Path, err)
			continue
		}
		allSymbols = append(allSymbols, symbols...)
	}

	if err := json.NewEncoder(os.Stdout).Encode(allSymbols); err != nil {
		fmt.Fprintf(os.Stderr, "encode output: %v\n", err)
		os.Exit(1)
	}
}

func parseFile(f InputFile) ([]Symbol, error) {
	src, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, err
	}

	lang := getLanguage(f.Lang)
	if lang == nil {
		return nil, fmt.Errorf("unsupported language: %s", f.Lang)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	return extractSymbols(root, src, f.Rel, f.Lang), nil
}

func getLanguage(lang string) *sitter.Language {
	switch lang {
	case "python":
		return python.GetLanguage()
	case "javascript", "jsx":
		return javascript.GetLanguage()
	case "typescript":
		return typescript.GetLanguage()
	case "tsx":
		return tsx.GetLanguage()
	case "rust":
		return rust.GetLanguage()
	case "java":
		return java.GetLanguage()
	default:
		return nil
	}
}

func extractSymbols(root *sitter.Node, src []byte, relPath, lang string) []Symbol {
	var symbols []Symbol

	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		syms := nodeToSymbols(child, src, relPath, lang)
		symbols = append(symbols, syms...)
	}

	return symbols
}

func nodeToSymbols(node *sitter.Node, src []byte, relPath, lang string) []Symbol {
	nodeType := node.Type()

	switch lang {
	case "python":
		return pythonSymbols(node, nodeType, src, relPath)
	case "javascript", "jsx", "typescript", "tsx":
		return jstsSymbols(node, nodeType, src, relPath)
	case "rust":
		return rustSymbols(node, nodeType, src, relPath)
	case "java":
		return javaSymbols(node, nodeType, src, relPath)
	}
	return nil
}

// pythonSymbols extracts Python top-level symbols.
func pythonSymbols(node *sitter.Node, nodeType string, src []byte, relPath string) []Symbol {
	switch nodeType {
	case "function_definition":
		name := childContent(node, "name", src)
		params := childContent(node, "parameters", src)
		return []Symbol{{
			Name:      name,
			Kind:      "func",
			Signature: "def " + name + params,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  !strings.HasPrefix(name, "_"),
		}}

	case "class_definition":
		name := childContent(node, "name", src)
		sym := Symbol{
			Name:      name,
			Kind:      "class",
			Signature: "class " + name,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  !strings.HasPrefix(name, "_"),
		}
		// Check for base classes.
		if args := childByField(node, "superclasses"); args != nil {
			sym.Signature += args.Content(src)
		}
		return []Symbol{sym}

	case "decorated_definition":
		// Unwrap decorator to get the actual definition.
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "function_definition" || child.Type() == "class_definition" {
				return pythonSymbols(child, child.Type(), src, relPath)
			}
		}
	}
	return nil
}

// jstsSymbols extracts JavaScript/TypeScript top-level symbols.
func jstsSymbols(node *sitter.Node, nodeType string, src []byte, relPath string) []Symbol {
	switch nodeType {
	case "function_declaration":
		name := childContent(node, "name", src)
		params := childContent(node, "parameters", src)
		return []Symbol{{
			Name:      name,
			Kind:      "func",
			Signature: "function " + name + params,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  true,
		}}

	case "class_declaration":
		name := childContent(node, "name", src)
		return []Symbol{{
			Name:      name,
			Kind:      "class",
			Signature: "class " + name,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  true,
		}}

	case "interface_declaration":
		name := childContent(node, "name", src)
		return []Symbol{{
			Name:      name,
			Kind:      "interface",
			Signature: "interface " + name,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  true,
		}}

	case "type_alias_declaration":
		name := childContent(node, "name", src)
		return []Symbol{{
			Name:      name,
			Kind:      "type",
			Signature: "type " + name,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  true,
		}}

	case "export_statement":
		// Unwrap export to get the declaration.
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			syms := jstsSymbols(child, child.Type(), src, relPath)
			for j := range syms {
				syms[j].Exported = true
			}
			if len(syms) > 0 {
				return syms
			}
		}

	case "lexical_declaration":
		// const/let declarations: extract variable names.
		var syms []Symbol
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "variable_declarator" {
				name := childContent(child, "name", src)
				if name != "" {
					syms = append(syms, Symbol{
						Name:     name,
						Kind:     "const",
						File:     relPath,
						Line:     int(child.StartPoint().Row) + 1,
						Exported: isJSExported(name),
					})
				}
			}
		}
		return syms
	}
	return nil
}

// rustSymbols extracts Rust top-level symbols.
func rustSymbols(node *sitter.Node, nodeType string, src []byte, relPath string) []Symbol {
	switch nodeType {
	case "function_item":
		name := childContent(node, "name", src)
		params := childContent(node, "parameters", src)
		vis := hasVisibility(node, src)
		return []Symbol{{
			Name:      name,
			Kind:      "func",
			Signature: "fn " + name + params,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  vis,
		}}

	case "struct_item":
		name := childContent(node, "name", src)
		return []Symbol{{
			Name:      name,
			Kind:      "type",
			Signature: "struct " + name,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  hasVisibility(node, src),
		}}

	case "enum_item":
		name := childContent(node, "name", src)
		return []Symbol{{
			Name:      name,
			Kind:      "type",
			Signature: "enum " + name,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  hasVisibility(node, src),
		}}

	case "trait_item":
		name := childContent(node, "name", src)
		return []Symbol{{
			Name:      name,
			Kind:      "interface",
			Signature: "trait " + name,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  hasVisibility(node, src),
		}}

	case "impl_item":
		// Extract methods from impl blocks.
		typeName := childContent(node, "type", src)
		var syms []Symbol
		if body := childByField(node, "body"); body != nil {
			for i := 0; i < int(body.ChildCount()); i++ {
				child := body.Child(i)
				if child.Type() == "function_item" {
					name := childContent(child, "name", src)
					params := childContent(child, "parameters", src)
					syms = append(syms, Symbol{
						Name:      name,
						Kind:      "method",
						Signature: "fn " + typeName + "::" + name + params,
						File:      relPath,
						Line:      int(child.StartPoint().Row) + 1,
						Exported:  hasVisibility(child, src),
					})
				}
			}
		}
		return syms
	}
	return nil
}

// javaSymbols extracts Java top-level symbols.
func javaSymbols(node *sitter.Node, nodeType string, src []byte, relPath string) []Symbol {
	switch nodeType {
	case "class_declaration":
		name := childContent(node, "name", src)
		return []Symbol{{
			Name:      name,
			Kind:      "class",
			Signature: "class " + name,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  isJavaPublic(node, src),
		}}

	case "interface_declaration":
		name := childContent(node, "name", src)
		return []Symbol{{
			Name:      name,
			Kind:      "interface",
			Signature: "interface " + name,
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  isJavaPublic(node, src),
		}}

	case "method_declaration":
		name := childContent(node, "name", src)
		params := childContent(node, "parameters", src)
		retType := childContent(node, "type", src)
		sig := retType + " " + name + params
		return []Symbol{{
			Name:      name,
			Kind:      "method",
			Signature: strings.TrimSpace(sig),
			File:      relPath,
			Line:      int(node.StartPoint().Row) + 1,
			Exported:  isJavaPublic(node, src),
		}}
	}
	return nil
}

// Helper functions for tree-sitter node traversal.

func childContent(node *sitter.Node, fieldName string, src []byte) string {
	child := node.ChildByFieldName(fieldName)
	if child == nil {
		return ""
	}
	return child.Content(src)
}

func childByField(node *sitter.Node, fieldName string) *sitter.Node {
	return node.ChildByFieldName(fieldName)
}

func hasVisibility(node *sitter.Node, src []byte) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "visibility_modifier" {
			return strings.Contains(child.Content(src), "pub")
		}
	}
	return false
}

func isJSExported(name string) bool {
	return len(name) > 0 && unicode.IsUpper(rune(name[0]))
}

func isJavaPublic(node *sitter.Node, src []byte) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "modifiers" {
			return strings.Contains(child.Content(src), "public")
		}
	}
	return false
}
