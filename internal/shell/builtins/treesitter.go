package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/treesitter"
)

func init() {
	Register(&symbolsVerb{})
	Register(&searchSymbolsVerb{})
	Register(&refsVerb{})
}

// supportedExtension is the same logic the treesitter package uses internally
// (langAliases maps extensions to canonical languages). Kept here so this
// file doesn't need an import-cycle-prone helper.
var supportedExtension = map[string]string{
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

func languageFromPath(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	return supportedExtension[ext]
}

// walkSourceFiles invokes fn for every supported source file under root.
// If root is a single file, fn is called once with that file. Skips .git,
// node_modules, vendor (parity with treesitter.Analyze).
func walkSourceFiles(root string, fn func(path, lang string) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if lang := languageFromPath(root); lang != "" {
			return fn(root, lang)
		}
		return fmt.Errorf("file %q has unsupported language", root)
	}

	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "node_modules" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if lang := languageFromPath(path); lang != "" {
			return fn(path, lang)
		}
		return nil
	})
}

// resolvePath turns a possibly-relative arg into an absolute path rooted at cwd.
func resolvePath(arg, cwd string) string {
	if filepath.IsAbs(arg) {
		return arg
	}
	if cwd == "" {
		return arg
	}
	return filepath.Join(cwd, arg)
}

// ----- yc symbols -----

type symbolsVerb struct{}

func (symbolsVerb) Name() string        { return "symbols" }
func (symbolsVerb) Description() string { return "List top-level symbols (func/type/class/...) in a file or directory" }
func (symbolsVerb) Usage() string       { return "yc symbols <path> [--json]" }

func (symbolsVerb) Run(ctx context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	asJSON := false
	var target string
	for _, a := range args {
		switch a {
		case "--json":
			asJSON = true
		default:
			if target == "" {
				target = a
			}
		}
	}
	if target == "" {
		fmt.Fprintln(stdio.Stderr, "yc symbols: missing path argument")
		return 2, nil
	}
	abs := resolvePath(target, cwd)
	parser := treesitter.NewParser()

	var all []treesitter.Symbol
	err := walkSourceFiles(abs, func(path, lang string) error {
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		tree, perr := parser.Parse(ctx, src, lang)
		if perr != nil {
			return nil
		}
		all = append(all, treesitter.ExtractSymbols(tree, path)...)
		return nil
	})
	if err != nil {
		return 1, err
	}

	if asJSON {
		enc := json.NewEncoder(stdio.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(all)
		return 0, nil
	}
	for _, s := range all {
		fmt.Fprintf(stdio.Stdout, "%s:%d: %-9s %s %s\n", s.File, s.Line, s.Kind, s.Name, s.Signature)
	}
	if len(all) == 0 {
		fmt.Fprintln(stdio.Stderr, "(no symbols found)")
	}
	return 0, nil
}

// ----- yc search-symbols -----

type searchSymbolsVerb struct{}

func (searchSymbolsVerb) Name() string        { return "search-symbols" }
func (searchSymbolsVerb) Description() string { return "Search for symbols whose name matches a substring/regex" }
func (searchSymbolsVerb) Usage() string       { return "yc search-symbols <pattern> [path] [--json]" }

func (searchSymbolsVerb) Run(ctx context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	asJSON := false
	var pattern, target string
	for _, a := range args {
		switch a {
		case "--json":
			asJSON = true
		default:
			if pattern == "" {
				pattern = a
			} else if target == "" {
				target = a
			}
		}
	}
	if pattern == "" {
		fmt.Fprintln(stdio.Stderr, "yc search-symbols: missing pattern")
		return 2, nil
	}
	if target == "" {
		target = "."
	}
	abs := resolvePath(target, cwd)
	parser := treesitter.NewParser()

	var matches []treesitter.Symbol
	needle := strings.ToLower(pattern)
	err := walkSourceFiles(abs, func(path, lang string) error {
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		tree, perr := parser.Parse(ctx, src, lang)
		if perr != nil {
			return nil
		}
		for _, s := range treesitter.ExtractSymbols(tree, path) {
			if strings.Contains(strings.ToLower(s.Name), needle) {
				matches = append(matches, s)
			}
		}
		return nil
	})
	if err != nil {
		return 1, err
	}

	if asJSON {
		enc := json.NewEncoder(stdio.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(matches)
		return 0, nil
	}
	for _, s := range matches {
		fmt.Fprintf(stdio.Stdout, "%s:%d: %-9s %s %s\n", s.File, s.Line, s.Kind, s.Name, s.Signature)
	}
	if len(matches) == 0 {
		fmt.Fprintln(stdio.Stderr, "(no symbols match)")
		return 1, nil
	}
	return 0, nil
}

// ----- yc refs -----

type refsVerb struct{}

func (refsVerb) Name() string        { return "refs" }
func (refsVerb) Description() string { return "Find references and callers of a symbol across the workspace" }
func (refsVerb) Usage() string       { return "yc refs <symbol> [workspace] [--json]" }

func (refsVerb) Run(ctx context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	asJSON := false
	var symbol, workspace string
	for _, a := range args {
		switch a {
		case "--json":
			asJSON = true
		default:
			if symbol == "" {
				symbol = a
			} else if workspace == "" {
				workspace = a
			}
		}
	}
	if symbol == "" {
		fmt.Fprintln(stdio.Stderr, "yc refs: missing symbol")
		return 2, nil
	}
	if workspace == "" {
		workspace = "."
	}
	abs := resolvePath(workspace, cwd)
	parser := treesitter.NewParser()

	// Analyze takes (symbol, targetFile, workspaceRoot). targetFile is
	// optional context (treesitter uses it to resolve language); we pass
	// empty so it scans every supported file.
	impacts, err := treesitter.Analyze(ctx, parser, symbol, "", abs)
	if err != nil {
		return 1, err
	}

	if asJSON {
		enc := json.NewEncoder(stdio.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(impacts)
		return 0, nil
	}
	for _, im := range impacts {
		fmt.Fprintf(stdio.Stdout, "%s:%d: %-12s %s\n", im.File, im.Line, im.Kind, im.Symbol)
		if im.Context != "" {
			fmt.Fprintf(stdio.Stdout, "    %s\n", strings.TrimSpace(im.Context))
		}
	}
	if len(impacts) == 0 {
		fmt.Fprintln(stdio.Stderr, "(no references found)")
		return 1, nil
	}
	return 0, nil
}
