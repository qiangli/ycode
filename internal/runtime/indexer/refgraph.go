package indexer

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/qiangli/ycode/internal/storage"
)

const (
	refBucket     = "references"
	callerPrefix  = "callers:"  // callers:pkg.Func -> [caller1, caller2, ...]
	calleePrefix  = "callees:"  // callees:pkg.Func -> [callee1, callee2, ...]
	definesPrefix = "defines:"  // defines:Func -> [file1:line, file2:line, ...]
)

// RefEdge represents a reference from one symbol to another.
type RefEdge struct {
	File string `json:"file"` // relative path of the reference site
	Line int    `json:"line"`
}

// RefGraph provides reference graph operations backed by a KV store.
type RefGraph struct {
	kv storage.KVStore
}

// NewRefGraph creates a reference graph backed by the given KV store.
func NewRefGraph(kv storage.KVStore) *RefGraph {
	if kv == nil {
		return nil
	}
	return &RefGraph{kv: kv}
}

// IndexFileReferences parses a Go file and records caller/callee edges.
func (g *RefGraph) IndexFileReferences(absPath, relPath string) {
	if g == nil {
		return
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, nil, 0)
	if err != nil {
		return
	}

	pkgName := f.Name.Name

	// Walk the AST to find function calls.
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Determine the callee name.
		callee := calleeNameFromExpr(call.Fun)
		if callee == "" {
			return true
		}

		// Determine the enclosing function (caller).
		callerName := enclosingFuncName(fset, f, call.Pos(), pkgName)
		if callerName == "" {
			callerName = pkgName + ".<init>" // top-level call
		}

		line := fset.Position(call.Pos()).Line

		// Record edges.
		g.addEdge(calleePrefix+callerName, callee)
		g.addEdge(callerPrefix+callee, callerName)
		g.addDefine(callee, relPath, line)

		return true
	})

	// Record definitions for all top-level symbols.
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := pkgName + "." + d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = pkgName + "." + d.Name.Name // simplified
			}
			line := fset.Position(d.Pos()).Line
			g.addDefine(name, relPath, line)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					name := pkgName + "." + ts.Name.Name
					line := fset.Position(ts.Pos()).Line
					g.addDefine(name, relPath, line)
				}
			}
		}
	}
}

// FindCallers returns all symbols that call the given symbol.
func (g *RefGraph) FindCallers(symbol string) []string {
	if g == nil {
		return nil
	}
	return g.getEdges(callerPrefix + symbol)
}

// FindCallees returns all symbols called by the given symbol.
func (g *RefGraph) FindCallees(symbol string) []string {
	if g == nil {
		return nil
	}
	return g.getEdges(calleePrefix + symbol)
}

// FindDefinitions returns file:line locations where a symbol is defined.
func (g *RefGraph) FindDefinitions(symbol string) []RefEdge {
	if g == nil {
		return nil
	}
	data, err := g.kv.Get(refBucket, definesPrefix+symbol)
	if err != nil || data == nil {
		return nil
	}
	var edges []RefEdge
	if err := json.Unmarshal(data, &edges); err != nil {
		return nil
	}
	return edges
}

// FindImpact traverses the reference graph N levels deep to find all
// symbols transitively affected by changing the given symbol.
func (g *RefGraph) FindImpact(symbol string, maxDepth int) []string {
	if g == nil {
		return nil
	}
	if maxDepth <= 0 {
		maxDepth = 3
	}

	visited := make(map[string]bool)
	var result []string
	queue := []string{symbol}
	visited[symbol] = true

	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		var next []string
		for _, sym := range queue {
			callers := g.FindCallers(sym)
			for _, caller := range callers {
				if !visited[caller] {
					visited[caller] = true
					result = append(result, caller)
					next = append(next, caller)
				}
			}
		}
		queue = next
	}

	return result
}

// addEdge appends a value to a JSON array stored at the given key.
func (g *RefGraph) addEdge(key, value string) {
	existing := g.getEdges(key)

	// Deduplicate.
	for _, e := range existing {
		if e == value {
			return
		}
	}

	existing = append(existing, value)
	data, _ := json.Marshal(existing)
	_ = g.kv.Put(refBucket, key, data)
}

// addDefine records a definition location.
func (g *RefGraph) addDefine(symbol, file string, line int) {
	key := definesPrefix + symbol
	var edges []RefEdge

	data, err := g.kv.Get(refBucket, key)
	if err == nil && data != nil {
		_ = json.Unmarshal(data, &edges)
	}

	// Deduplicate.
	for _, e := range edges {
		if e.File == file && e.Line == line {
			return
		}
	}

	edges = append(edges, RefEdge{File: file, Line: line})
	data, _ = json.Marshal(edges)
	_ = g.kv.Put(refBucket, key, data)
}

// getEdges reads a JSON string array from the KV store.
func (g *RefGraph) getEdges(key string) []string {
	data, err := g.kv.Get(refBucket, key)
	if err != nil || data == nil {
		return nil
	}
	var edges []string
	if err := json.Unmarshal(data, &edges); err != nil {
		return nil
	}
	return edges
}

// calleeNameFromExpr extracts the function name from a call expression.
func calleeNameFromExpr(expr ast.Expr) string {
	switch fn := expr.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	default:
		return ""
	}
}

// enclosingFuncName finds the name of the function that contains the given position.
func enclosingFuncName(fset *token.FileSet, f *ast.File, pos token.Pos, pkgName string) string {
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fd.Body == nil {
			continue
		}
		start := fd.Body.Pos()
		end := fd.Body.End()
		if pos >= start && pos <= end {
			name := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				// Include receiver type for methods.
				recv := fd.Recv.List[0]
				rName := typeNameFromExpr(recv.Type)
				if rName != "" {
					return pkgName + "." + rName + "." + name
				}
			}
			return pkgName + "." + name
		}
	}
	return ""
}

func typeNameFromExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeNameFromExpr(t.X)
	default:
		return ""
	}
}

// SymbolMatches returns all symbols in the reference graph matching a prefix.
func (g *RefGraph) SymbolMatches(prefix string) []string {
	if g == nil {
		return nil
	}
	// Scan defines: keys for matching symbols.
	keys, err := g.kv.List(refBucket)
	if err != nil {
		return nil
	}
	var matches []string
	for _, key := range keys {
		if strings.HasPrefix(key, definesPrefix) {
			sym := strings.TrimPrefix(key, definesPrefix)
			if strings.Contains(strings.ToLower(sym), strings.ToLower(prefix)) {
				matches = append(matches, sym)
			}
		}
	}
	return matches
}
