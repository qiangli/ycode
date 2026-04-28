package indexer

import (
	"bufio"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/qiangli/ycode/internal/storage"
)

const symbolIndexName = "symbols"

// SymbolDoc represents a symbol indexed into Bleve.
type SymbolDoc struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`      // func, type, interface, method, class, const, var
	File      string `json:"file"`      // relative path
	Language  string `json:"language"`  // go, py, ts, etc.
	Signature string `json:"signature"` // human-readable signature
	Line      int    `json:"line"`
	Exported  bool   `json:"exported"`
}

// IndexSymbols extracts symbols from a source file and indexes them in Bleve.
func (idx *Indexer) IndexSymbols(ctx context.Context, relPath, absPath, lang string) error {
	var symbols []SymbolDoc

	switch lang {
	case "go":
		symbols = extractGoSymbols(absPath, relPath)
	default:
		symbols = extractRegexSymbols(absPath, relPath, lang)
	}

	if len(symbols) == 0 {
		return nil
	}

	var docs []storage.Document
	for _, sym := range symbols {
		docID := fmt.Sprintf("%s:%s:%d", relPath, sym.Name, sym.Line)
		exported := "false"
		if sym.Exported {
			exported = "true"
		}
		docs = append(docs, storage.Document{
			ID:      docID,
			Content: sym.Name + " " + sym.Signature,
			Metadata: map[string]string{
				"name":      sym.Name,
				"kind":      sym.Kind,
				"file":      relPath,
				"language":  lang,
				"signature": sym.Signature,
				"line":      fmt.Sprintf("%d", sym.Line),
				"exported":  exported,
			},
		})
	}

	return idx.search.BatchIndex(ctx, symbolIndexName, docs)
}

// extractGoSymbols parses a Go file and returns top-level symbols.
func extractGoSymbols(absPath, relPath string) []SymbolDoc {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, nil, 0)
	if err != nil {
		return nil
	}

	var symbols []SymbolDoc

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := SymbolDoc{
				Name:     d.Name.Name,
				Kind:     "func",
				File:     relPath,
				Language: "go",
				Line:     fset.Position(d.Pos()).Line,
				Exported: isExportedName(d.Name.Name),
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				sym.Kind = "method"
			}
			sym.Signature = sym.Kind + " " + d.Name.Name
			symbols = append(symbols, sym)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					kind := "type"
					if _, ok := s.Type.(*ast.InterfaceType); ok {
						kind = "interface"
					}
					symbols = append(symbols, SymbolDoc{
						Name:      s.Name.Name,
						Kind:      kind,
						File:      relPath,
						Language:  "go",
						Signature: "type " + s.Name.Name,
						Line:      fset.Position(s.Pos()).Line,
						Exported:  isExportedName(s.Name.Name),
					})
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, name := range s.Names {
						symbols = append(symbols, SymbolDoc{
							Name:     name.Name,
							Kind:     kind,
							File:     relPath,
							Language: "go",
							Line:     fset.Position(name.Pos()).Line,
							Exported: isExportedName(name.Name),
						})
					}
				}
			}
		}
	}

	return symbols
}

func isExportedName(name string) bool {
	if name == "" {
		return false
	}
	return unicode.IsUpper(rune(name[0]))
}

// Language-specific regex patterns for symbol extraction.
var langPatterns = map[string][]*symbolPattern{
	"py": {
		{re: regexp.MustCompile(`^(?:async\s+)?def\s+(\w+)`), kind: "func"},
		{re: regexp.MustCompile(`^class\s+(\w+)`), kind: "class"},
	},
	"js": {
		{re: regexp.MustCompile(`^(?:export\s+)?function\s+(\w+)`), kind: "func"},
		{re: regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)`), kind: "class"},
		{re: regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:function|\()`), kind: "func"},
	},
	"ts": {
		{re: regexp.MustCompile(`^(?:export\s+)?function\s+(\w+)`), kind: "func"},
		{re: regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)`), kind: "class"},
		{re: regexp.MustCompile(`^(?:export\s+)?interface\s+(\w+)`), kind: "interface"},
		{re: regexp.MustCompile(`^(?:export\s+)?type\s+(\w+)`), kind: "type"},
		{re: regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:function|\()`), kind: "func"},
	},
	"tsx": nil, // filled in init
	"jsx": nil, // filled in init
	"rs": {
		{re: regexp.MustCompile(`^(?:pub\s+)?fn\s+(\w+)`), kind: "func"},
		{re: regexp.MustCompile(`^(?:pub\s+)?struct\s+(\w+)`), kind: "type"},
		{re: regexp.MustCompile(`^(?:pub\s+)?enum\s+(\w+)`), kind: "type"},
		{re: regexp.MustCompile(`^(?:pub\s+)?trait\s+(\w+)`), kind: "interface"},
		{re: regexp.MustCompile(`^impl(?:<[^>]*>)?\s+(\w+)`), kind: "type"},
	},
	"java": {
		{re: regexp.MustCompile(`^(?:public|private|protected)?\s*(?:static\s+)?(?:abstract\s+)?class\s+(\w+)`), kind: "class"},
		{re: regexp.MustCompile(`^(?:public|private|protected)?\s*interface\s+(\w+)`), kind: "interface"},
		{re: regexp.MustCompile(`^(?:public|private|protected)?\s*enum\s+(\w+)`), kind: "type"},
	},
	"rb": {
		{re: regexp.MustCompile(`^\s*def\s+(\w+)`), kind: "func"},
		{re: regexp.MustCompile(`^\s*class\s+(\w+)`), kind: "class"},
		{re: regexp.MustCompile(`^\s*module\s+(\w+)`), kind: "type"},
	},
}

func init() {
	langPatterns["tsx"] = langPatterns["ts"]
	langPatterns["jsx"] = langPatterns["js"]
}

type symbolPattern struct {
	re   *regexp.Regexp
	kind string
}

// extractRegexSymbols uses regex patterns to extract symbols from non-Go files.
func extractRegexSymbols(absPath, relPath, lang string) []SymbolDoc {
	patterns := langPatterns[lang]
	if len(patterns) == 0 {
		return nil
	}

	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var symbols []SymbolDoc
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		for _, p := range patterns {
			m := p.re.FindStringSubmatch(trimmed)
			if m != nil && len(m) > 1 {
				name := m[1]
				exported := isExportedName(name)
				// Python/Ruby: underscore prefix = private.
				if (lang == "py" || lang == "rb") && strings.HasPrefix(name, "_") {
					exported = false
				}
				// JS/TS: export keyword = exported.
				if (lang == "js" || lang == "ts" || lang == "tsx" || lang == "jsx") &&
					strings.HasPrefix(trimmed, "export") {
					exported = true
				}
				symbols = append(symbols, SymbolDoc{
					Name:      name,
					Kind:      p.kind,
					File:      relPath,
					Language:  lang,
					Signature: p.kind + " " + name,
					Line:      lineNum,
					Exported:  exported,
				})
				break // one match per line
			}
		}
	}

	return symbols
}
