package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/qiangli/ycode/internal/runtime/vfs"
)

// NotebookCell represents a Jupyter notebook cell.
type NotebookCell struct {
	CellType string         `json:"cell_type"`
	Source   []string       `json:"source"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Outputs  []any          `json:"outputs,omitempty"`
}

// Notebook represents a Jupyter notebook.
type Notebook struct {
	Cells         []NotebookCell `json:"cells"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	NBFormat      int            `json:"nbformat"`
	NBFormatMinor int            `json:"nbformat_minor"`
}

// RegisterNotebookHandler registers the NotebookEdit tool handler with VFS path validation.
func RegisterNotebookHandler(r *Registry, v *vfs.VFS) {
	spec, ok := r.Get("NotebookEdit")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			NotebookPath string `json:"notebook_path"`
			CellIndex    int    `json:"cell_index"`
			Action       string `json:"action"` // replace, insert, delete
			Content      string `json:"content,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse NotebookEdit input: %w", err)
		}

		absPath, err := v.ValidatePath(ctx, params.NotebookPath)
		if err != nil {
			return "", err
		}
		params.NotebookPath = absPath

		data, err := os.ReadFile(params.NotebookPath)
		if err != nil {
			return "", fmt.Errorf("read notebook: %w", err)
		}

		var nb Notebook
		if err := json.Unmarshal(data, &nb); err != nil {
			return "", fmt.Errorf("parse notebook: %w", err)
		}

		switch params.Action {
		case "replace":
			if params.CellIndex < 0 || params.CellIndex >= len(nb.Cells) {
				return "", fmt.Errorf("cell index %d out of range (0-%d)", params.CellIndex, len(nb.Cells)-1)
			}
			nb.Cells[params.CellIndex].Source = []string{params.Content}
		case "insert":
			if params.CellIndex < 0 || params.CellIndex > len(nb.Cells) {
				return "", fmt.Errorf("cell index %d out of range (0-%d)", params.CellIndex, len(nb.Cells))
			}
			newCell := NotebookCell{
				CellType: "code",
				Source:   []string{params.Content},
				Metadata: map[string]any{},
			}
			nb.Cells = append(nb.Cells[:params.CellIndex], append([]NotebookCell{newCell}, nb.Cells[params.CellIndex:]...)...)
		case "delete":
			if params.CellIndex < 0 || params.CellIndex >= len(nb.Cells) {
				return "", fmt.Errorf("cell index %d out of range (0-%d)", params.CellIndex, len(nb.Cells)-1)
			}
			nb.Cells = append(nb.Cells[:params.CellIndex], nb.Cells[params.CellIndex+1:]...)
		default:
			return "", fmt.Errorf("unknown action: %s", params.Action)
		}

		out, err := json.MarshalIndent(nb, "", " ")
		if err != nil {
			return "", fmt.Errorf("marshal notebook: %w", err)
		}
		if err := os.WriteFile(params.NotebookPath, out, 0o644); err != nil {
			return "", fmt.Errorf("write notebook: %w", err)
		}
		r.NotifyFileWrite(params.NotebookPath)

		return fmt.Sprintf("notebook %s: %s cell at index %d", params.NotebookPath, params.Action, params.CellIndex), nil
	}
}
