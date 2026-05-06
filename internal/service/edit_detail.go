package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// EditDetail describes a proposed file mutation, suitable for sending to a
// remote client so it can render a diff preview alongside the permission
// request. Both texts are full file contents (UTF-8); for very large files
// the consumer is expected to virtualise display.
type EditDetail struct {
	FilePath   string `json:"file_path"`
	BeforeText string `json:"before_text"`
	AfterText  string `json:"after_text"`
}

// extractEditDetail returns a populated EditDetail when toolName is a
// file-mutation tool whose input we can interpret structurally. Returns
// nil for non-edit tools, parse failures, or paths that resolve outside
// the workspace.
//
// For edit_file, the after-text is computed by replaying the same string
// substitution the handler will perform; if the old_string is not present
// in the existing file, after_text is left equal to before_text and the
// client is expected to surface that as a no-op. Reading the file here is
// a best-effort preview — the handler still re-reads at execution time
// and is the source of truth for what actually gets written.
func extractEditDetail(toolName string, input json.RawMessage, workDir string) *EditDetail {
	switch toolName {
	case "write_file":
		var p struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(input, &p); err != nil || p.FilePath == "" {
			return nil
		}
		abs := resolveEditPath(p.FilePath, workDir)
		before, _ := os.ReadFile(abs) // missing file is OK — empty before
		return &EditDetail{
			FilePath:   p.FilePath,
			BeforeText: string(before),
			AfterText:  p.Content,
		}

	case "edit_file":
		var p struct {
			FilePath   string `json:"file_path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err := json.Unmarshal(input, &p); err != nil || p.FilePath == "" {
			return nil
		}
		abs := resolveEditPath(p.FilePath, workDir)
		beforeBytes, err := os.ReadFile(abs)
		if err != nil {
			return nil
		}
		before := string(beforeBytes)
		var after string
		switch {
		case p.OldString == "":
			after = before
		case p.ReplaceAll:
			after = strings.ReplaceAll(before, p.OldString, p.NewString)
		default:
			after = strings.Replace(before, p.OldString, p.NewString, 1)
		}
		return &EditDetail{
			FilePath:   p.FilePath,
			BeforeText: before,
			AfterText:  after,
		}
	}
	return nil
}

func resolveEditPath(p, workDir string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(workDir, p)
}
