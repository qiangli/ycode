package repomap

// Tree-sitter container build context stored as string constants.
// These are compiled into the ycode binary and written to a temp directory
// when the container image needs to be built. This avoids go:embed
// restrictions across module boundaries.

const treesitterDockerfile = `FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev git

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /ts-symbols .

FROM alpine:3.21
COPY --from=builder /ts-symbols /usr/local/bin/ts-symbols
ENTRYPOINT ["/usr/local/bin/ts-symbols"]
`

const treesitterGoMod = `module github.com/qiangli/ycode/ts-symbols

go 1.24

require github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82
`

// treesitterMainGo is the source code for the containerized tree-sitter
// symbol extractor. It reads a JSON manifest from stdin and writes
// a JSON array of symbols to stdout.
//
// The canonical source is in treesitter/main.go for development reference.
// When updating the parser, edit treesitter/main.go and copy the content here.
const treesitterMainGo = `// ts-symbols extracts top-level symbols from source files using tree-sitter.
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
	Path string ` + "`" + `json:"path"` + "`" + `
	Rel  string ` + "`" + `json:"rel"` + "`" + `
	Lang string ` + "`" + `json:"lang"` + "`" + `
}

type Input struct {
	Files []InputFile ` + "`" + `json:"files"` + "`" + `
}

type Symbol struct {
	Name      string ` + "`" + `json:"name"` + "`" + `
	Kind      string ` + "`" + `json:"kind"` + "`" + `
	Signature string ` + "`" + `json:"signature,omitempty"` + "`" + `
	File      string ` + "`" + `json:"file"` + "`" + `
	Line      int    ` + "`" + `json:"line"` + "`" + `
	Exported  bool   ` + "`" + `json:"exported"` + "`" + `
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
	return extractNodeSymbols(root, src, f.Rel, f.Lang), nil
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

func extractNodeSymbols(root *sitter.Node, src []byte, relPath, lang string) []Symbol {
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

func pythonSymbols(node *sitter.Node, nodeType string, src []byte, relPath string) []Symbol {
	switch nodeType {
	case "function_definition":
		name := childContent(node, "name", src)
		params := childContent(node, "parameters", src)
		return []Symbol{{Name: name, Kind: "func", Signature: "def " + name + params, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: !strings.HasPrefix(name, "_")}}
	case "class_definition":
		name := childContent(node, "name", src)
		sig := "class " + name
		if args := node.ChildByFieldName("superclasses"); args != nil {
			sig += args.Content(src)
		}
		return []Symbol{{Name: name, Kind: "class", Signature: sig, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: !strings.HasPrefix(name, "_")}}
	case "decorated_definition":
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "function_definition" || child.Type() == "class_definition" {
				return pythonSymbols(child, child.Type(), src, relPath)
			}
		}
	}
	return nil
}

func jstsSymbols(node *sitter.Node, nodeType string, src []byte, relPath string) []Symbol {
	switch nodeType {
	case "function_declaration":
		name := childContent(node, "name", src)
		params := childContent(node, "parameters", src)
		return []Symbol{{Name: name, Kind: "func", Signature: "function " + name + params, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: true}}
	case "class_declaration":
		name := childContent(node, "name", src)
		return []Symbol{{Name: name, Kind: "class", Signature: "class " + name, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: true}}
	case "interface_declaration":
		name := childContent(node, "name", src)
		return []Symbol{{Name: name, Kind: "interface", Signature: "interface " + name, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: true}}
	case "type_alias_declaration":
		name := childContent(node, "name", src)
		return []Symbol{{Name: name, Kind: "type", Signature: "type " + name, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: true}}
	case "export_statement":
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			syms := jstsSymbols(child, child.Type(), src, relPath)
			for j := range syms { syms[j].Exported = true }
			if len(syms) > 0 { return syms }
		}
	case "lexical_declaration":
		var syms []Symbol
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "variable_declarator" {
				name := childContent(child, "name", src)
				if name != "" {
					syms = append(syms, Symbol{Name: name, Kind: "const", File: relPath, Line: int(child.StartPoint().Row) + 1, Exported: len(name) > 0 && unicode.IsUpper(rune(name[0]))})
				}
			}
		}
		return syms
	}
	return nil
}

func rustSymbols(node *sitter.Node, nodeType string, src []byte, relPath string) []Symbol {
	switch nodeType {
	case "function_item":
		name := childContent(node, "name", src)
		params := childContent(node, "parameters", src)
		return []Symbol{{Name: name, Kind: "func", Signature: "fn " + name + params, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: hasVis(node, src)}}
	case "struct_item":
		name := childContent(node, "name", src)
		return []Symbol{{Name: name, Kind: "type", Signature: "struct " + name, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: hasVis(node, src)}}
	case "enum_item":
		name := childContent(node, "name", src)
		return []Symbol{{Name: name, Kind: "type", Signature: "enum " + name, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: hasVis(node, src)}}
	case "trait_item":
		name := childContent(node, "name", src)
		return []Symbol{{Name: name, Kind: "interface", Signature: "trait " + name, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: hasVis(node, src)}}
	case "impl_item":
		typeName := childContent(node, "type", src)
		var syms []Symbol
		if body := node.ChildByFieldName("body"); body != nil {
			for i := 0; i < int(body.ChildCount()); i++ {
				child := body.Child(i)
				if child.Type() == "function_item" {
					name := childContent(child, "name", src)
					params := childContent(child, "parameters", src)
					syms = append(syms, Symbol{Name: name, Kind: "method", Signature: "fn " + typeName + "::" + name + params, File: relPath, Line: int(child.StartPoint().Row) + 1, Exported: hasVis(child, src)})
				}
			}
		}
		return syms
	}
	return nil
}

func javaSymbols(node *sitter.Node, nodeType string, src []byte, relPath string) []Symbol {
	switch nodeType {
	case "class_declaration":
		name := childContent(node, "name", src)
		return []Symbol{{Name: name, Kind: "class", Signature: "class " + name, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: isPublic(node, src)}}
	case "interface_declaration":
		name := childContent(node, "name", src)
		return []Symbol{{Name: name, Kind: "interface", Signature: "interface " + name, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: isPublic(node, src)}}
	case "method_declaration":
		name := childContent(node, "name", src)
		params := childContent(node, "parameters", src)
		retType := childContent(node, "type", src)
		sig := strings.TrimSpace(retType + " " + name + params)
		return []Symbol{{Name: name, Kind: "method", Signature: sig, File: relPath, Line: int(node.StartPoint().Row) + 1, Exported: isPublic(node, src)}}
	}
	return nil
}

func childContent(node *sitter.Node, fieldName string, src []byte) string {
	child := node.ChildByFieldName(fieldName)
	if child == nil { return "" }
	return child.Content(src)
}

func hasVis(node *sitter.Node, src []byte) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "visibility_modifier" { return strings.Contains(child.Content(src), "pub") }
	}
	return false
}

func isPublic(node *sitter.Node, src []byte) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "modifiers" { return strings.Contains(child.Content(src), "public") }
	}
	return false
}
`
