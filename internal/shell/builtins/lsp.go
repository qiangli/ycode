package builtins

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/lsp"
)

func init() { Register(&lspVerb{}) }

type lspVerb struct{}

func (lspVerb) Name() string { return "lsp" }
func (lspVerb) Description() string {
	return "Query a language server (hover/definition/references/symbols/diagnostics)"
}
func (lspVerb) Usage() string {
	return "yc lsp <hover|definition|references|symbols|diagnostics> <file>[:<line>[:<col>]] [--json]"
}

func (lspVerb) Run(_ context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	if len(args) < 2 {
		fmt.Fprintln(stdio.Stderr, "yc lsp: usage:", (lspVerb{}).Usage())
		return 2, nil
	}

	action := args[0]
	target := args[1]

	fs := flag.NewFlagSet("yc lsp", flag.ContinueOnError)
	fs.SetOutput(stdio.Stderr)
	jsonOut := fs.Bool("json", false, "emit Response as JSON")
	language := fs.String("language", "", "force language (default: detected from file extension)")
	if err := fs.Parse(args[2:]); err != nil {
		return 2, nil
	}

	file, line, col, err := parseLSPTarget(target)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc lsp: %v\n", err)
		return 2, nil
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(cwd, file)
	}

	lang := *language
	if lang == "" {
		lang = detectLangFromExt(file)
	}
	if lang == "" {
		fmt.Fprintf(stdio.Stderr, "yc lsp: cannot detect language for %s; pass --language\n", file)
		return 2, nil
	}

	client, err := acquireLSPClient(lang, cwd)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc lsp: %v\n", err)
		return 1, nil
	}
	defer client.Close()

	req := &lsp.Request{
		Action:   lsp.Action(action),
		FilePath: file,
		Line:     line,
		Col:      col,
		Language: lang,
	}
	resp, err := lsp.Execute(client, req)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc lsp: %s: %v\n", action, err)
		return 1, nil
	}

	if *jsonOut {
		enc := json.NewEncoder(stdio.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			return 1, err
		}
	} else {
		fmt.Fprintln(stdio.Stdout, lsp.FormatResponse(resp))
	}
	return 0, nil
}

// parseLSPTarget accepts "file", "file:line", or "file:line:col" (lines
// and columns are 1-indexed for users; LSP wants 0-indexed). Returns
// 0-indexed line/col so the LSP client doesn't need to translate.
func parseLSPTarget(s string) (file string, line, col int, err error) {
	parts := strings.Split(s, ":")
	file = parts[0]
	if file == "" {
		return "", 0, 0, fmt.Errorf("empty file path in target %q", s)
	}
	if len(parts) >= 2 {
		n, perr := strconv.Atoi(parts[1])
		if perr != nil || n < 1 {
			return "", 0, 0, fmt.Errorf("invalid line %q (want 1-indexed integer)", parts[1])
		}
		line = n - 1
	}
	if len(parts) >= 3 {
		n, perr := strconv.Atoi(parts[2])
		if perr != nil || n < 1 {
			return "", 0, 0, fmt.Errorf("invalid column %q (want 1-indexed integer)", parts[2])
		}
		col = n - 1
	}
	return file, line, col, nil
}

func detectLangFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc":
		return "cpp"
	}
	return ""
}

// acquireLSPClient finds a server config for lang via AutoDetectServers,
// constructs a client, and points it at cwd. Returned client must be
// Close()d by the caller — each `yc lsp` invocation is short-lived, so
// we don't share a long-running registry.
func acquireLSPClient(lang, cwd string) (*lsp.Client, error) {
	for _, cfg := range lsp.AutoDetectServers() {
		if cfg.Language == lang {
			client := lsp.NewClient(cfg)
			client.SetRootDir(cwd)
			return client, nil
		}
	}
	return nil, fmt.Errorf("no LSP server detected for language %q (install gopls/pylsp/typescript-language-server)", lang)
}
