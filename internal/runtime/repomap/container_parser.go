package repomap

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/container"
	"github.com/qiangli/ycode/internal/runtime/containertool"
)

const treesitterImage = "ycode-treesitter:latest"

// treesitterInputFile is the input format for the containerized parser.
type treesitterInputFile struct {
	Path string `json:"path"` // path inside container (/workspace/...)
	Rel  string `json:"rel"`  // relative path for output
	Lang string `json:"lang"` // language name
}

type treesitterInput struct {
	Files []treesitterInputFile `json:"files"`
}

// treesitterSymbol matches the JSON output from the container.
type treesitterSymbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Signature string `json:"signature,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Exported  bool   `json:"exported"`
}

// newTreeSitterTool creates the container tool for tree-sitter parsing.
func newTreeSitterTool(workspaceRoot string, engine *container.Engine) *containertool.Tool {
	return &containertool.Tool{
		Name:       "treesitter",
		Image:      treesitterImage,
		Dockerfile: treesitterDockerfile,
		Sources: map[string]string{
			"main.go": treesitterMainGo,
			"go.mod":  treesitterGoMod,
			"go.sum":  "", // populated by go mod download inside the build
		},
		Mounts: []containertool.Mount{
			{Source: workspaceRoot, Target: "/workspace", ReadOnly: true},
		},
		Engine: engine,
	}
}

// parseFilesWithTreeSitter runs the containerized parser on non-Go source files.
func parseFilesWithTreeSitter(ctx context.Context, root string, files []fileInfo, engine *container.Engine) ([]Symbol, error) {
	if len(files) == 0 {
		return nil, nil
	}

	tool := newTreeSitterTool(root, engine)

	// Build input manifest.
	var input treesitterInput
	for _, f := range files {
		input.Files = append(input.Files, treesitterInputFile{
			Path: filepath.Join("/workspace", f.rel),
			Rel:  f.rel,
			Lang: f.lang,
		})
	}

	var tsSymbols []treesitterSymbol
	if err := tool.RunJSON(ctx, input, &tsSymbols); err != nil {
		return nil, err
	}

	// Convert to repomap.Symbol.
	symbols := make([]Symbol, len(tsSymbols))
	for i, ts := range tsSymbols {
		symbols[i] = Symbol{
			Name:      ts.Name,
			Kind:      ts.Kind,
			Signature: ts.Signature,
			File:      ts.File,
			Line:      ts.Line,
			Exported:  ts.Exported,
		}
	}

	return symbols, nil
}

// fileInfo is used to pass file metadata to the container parser.
type fileInfo struct {
	path string // absolute path on host
	rel  string // relative to repo root
	lang string // language name for tree-sitter
}

// langForExt returns the tree-sitter language name for a file extension.
func langForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".jsx":
		return "jsx"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	default:
		return ""
	}
}
