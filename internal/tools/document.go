package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/nguyenthenguyen/docx"
	"github.com/xuri/excelize/v2"

	"github.com/qiangli/ycode/internal/runtime/vfs"
)

// maxDocumentSize is the maximum document file size (50 MB).
const maxDocumentSize = 50 * 1024 * 1024

// supportedDocExts maps file extensions to document types.
var supportedDocExts = map[string]string{
	".pdf":  "pdf",
	".docx": "docx",
	".xlsx": "xlsx",
	".pptx": "pptx",
	".csv":  "csv",
}

// RegisterDocumentHandler registers the read_document tool handler.
func RegisterDocumentHandler(r *Registry, v *vfs.VFS) {
	spec, ok := r.Get("read_document")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			FilePath string `json:"file_path"`
			Pages    string `json:"pages,omitempty"` // e.g., "1-5", "3", "1,3,5"
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse read_document input: %w", err)
		}
		if params.FilePath == "" {
			return "", fmt.Errorf("file_path is required")
		}

		absPath, err := v.ValidatePath(ctx, params.FilePath)
		if err != nil {
			return "", err
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", absPath, err)
		}
		if info.Size() > maxDocumentSize {
			return "", fmt.Errorf("document too large: %d bytes (max %d)", info.Size(), maxDocumentSize)
		}

		ext := strings.ToLower(filepath.Ext(absPath))
		docType, ok := supportedDocExts[ext]
		if !ok {
			return "", fmt.Errorf("unsupported document type %q; supported: pdf, docx, xlsx, pptx, csv", ext)
		}

		switch docType {
		case "pdf":
			return readPDF(absPath, params.Pages)
		case "docx":
			return readDOCX(absPath)
		case "xlsx":
			return readXLSX(absPath)
		case "csv":
			return readCSV(absPath)
		case "pptx":
			return readPPTX(absPath)
		default:
			return "", fmt.Errorf("unsupported document type: %s", docType)
		}
	}
}

// readPDF extracts text content from a PDF file.
func readPDF(path, pages string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("open PDF: %w", err)
	}
	defer f.Close()

	totalPages := r.NumPage()
	if totalPages == 0 {
		return "PDF has no pages.", nil
	}

	// Parse page range if specified.
	pageSet := parsePageRange(pages, totalPages)

	var b strings.Builder
	fmt.Fprintf(&b, "PDF: %s (%d pages)\n\n", filepath.Base(path), totalPages)

	for i := 1; i <= totalPages; i++ {
		if pageSet != nil {
			if _, ok := pageSet[i]; !ok {
				continue
			}
		}

		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			fmt.Fprintf(&b, "--- Page %d ---\n[Error extracting text: %v]\n\n", i, err)
			continue
		}

		text = strings.TrimSpace(text)
		if text != "" {
			fmt.Fprintf(&b, "--- Page %d ---\n%s\n\n", i, text)
		}

		// Limit output to ~100KB.
		if b.Len() > 100*1024 {
			fmt.Fprintf(&b, "\n... (output truncated at page %d)\n", i)
			break
		}
	}

	result := b.String()
	if strings.TrimSpace(result) == fmt.Sprintf("PDF: %s (%d pages)", filepath.Base(path), totalPages) {
		return fmt.Sprintf("PDF: %s (%d pages) — no extractable text found (may be image-based).", filepath.Base(path), totalPages), nil
	}
	return result, nil
}

// readDOCX extracts text content from a DOCX file.
func readDOCX(path string) (string, error) {
	r, err := docx.ReadDocxFile(path)
	if err != nil {
		return "", fmt.Errorf("open DOCX: %w", err)
	}
	defer r.Close()

	doc := r.Editable()
	content := doc.GetContent()

	// Clean up XML tags to get plain text.
	content = strings.ReplaceAll(content, "</w:p>", "\n")
	content = strings.ReplaceAll(content, "</w:tr>", "\n")

	// Strip remaining XML tags.
	var clean strings.Builder
	inTag := false
	for _, ch := range content {
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			continue
		}
		if !inTag {
			clean.WriteRune(ch)
		}
	}

	text := strings.TrimSpace(clean.String())
	if text == "" {
		return fmt.Sprintf("DOCX: %s — no extractable text found.", filepath.Base(path)), nil
	}

	// Truncate if needed.
	if len(text) > 100*1024 {
		text = text[:100*1024] + "\n... (output truncated)"
	}

	return fmt.Sprintf("DOCX: %s\n\n%s", filepath.Base(path), text), nil
}

// readXLSX extracts content from an Excel file.
func readXLSX(path string) (string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", fmt.Errorf("open XLSX: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return "XLSX: no sheets found.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "XLSX: %s (%d sheets)\n\n", filepath.Base(path), len(sheets))

	for _, sheet := range sheets {
		rows, err := f.GetRows(sheet)
		if err != nil {
			fmt.Fprintf(&b, "--- Sheet: %s ---\n[Error reading: %v]\n\n", sheet, err)
			continue
		}

		if len(rows) == 0 {
			continue
		}

		fmt.Fprintf(&b, "--- Sheet: %s (%d rows) ---\n", sheet, len(rows))

		// Render as a simple table.
		for i, row := range rows {
			if i > 500 {
				fmt.Fprintf(&b, "... (%d more rows)\n", len(rows)-500)
				break
			}
			b.WriteString(strings.Join(row, "\t"))
			b.WriteString("\n")
		}
		b.WriteString("\n")

		if b.Len() > 100*1024 {
			fmt.Fprintf(&b, "\n... (output truncated)\n")
			break
		}
	}

	return b.String(), nil
}

// readCSV reads a CSV file as plain text (it's already text).
func readCSV(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read CSV: %w", err)
	}

	text := string(data)
	if len(text) > 100*1024 {
		text = text[:100*1024] + "\n... (output truncated)"
	}

	return fmt.Sprintf("CSV: %s\n\n%s", filepath.Base(path), text), nil
}

// readPPTX extracts text from a PPTX file using ZIP-based XML parsing.
func readPPTX(path string) (string, error) {
	// PPTX is a ZIP archive containing XML slide files.
	// We extract text from slide*.xml files.
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open PPTX: %w", err)
	}
	defer f.Close()

	info, _ := f.Stat()
	zipReader, err := newZIPReader(f, info.Size())
	if err != nil {
		return "", fmt.Errorf("open PPTX as ZIP: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "PPTX: %s\n\n", filepath.Base(path))

	slideNum := 0
	for _, zf := range zipReader.File {
		// Slides are in ppt/slides/slide*.xml.
		if !strings.HasPrefix(zf.Name, "ppt/slides/slide") || !strings.HasSuffix(zf.Name, ".xml") {
			continue
		}

		slideNum++
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		data, err := readAll(rc, 1024*1024) // 1MB per slide max
		rc.Close()
		if err != nil {
			continue
		}

		// Strip XML tags to get plain text.
		text := stripXMLTags(string(data))
		text = strings.TrimSpace(text)
		if text != "" {
			fmt.Fprintf(&b, "--- Slide %d ---\n%s\n\n", slideNum, text)
		}
	}

	if slideNum == 0 {
		return fmt.Sprintf("PPTX: %s — no slides found.", filepath.Base(path)), nil
	}

	result := b.String()
	if len(result) > 100*1024 {
		result = result[:100*1024] + "\n... (output truncated)"
	}
	return result, nil
}

// parsePageRange parses a page range string like "1-5", "3", "1,3,5" into a set of page numbers.
func parsePageRange(pages string, totalPages int) map[int]bool {
	if pages == "" {
		return nil
	}

	result := make(map[int]bool)
	for _, part := range strings.Split(pages, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			var start, end int
			if _, err := fmt.Sscanf(part, "%d-%d", &start, &end); err == nil {
				if start < 1 {
					start = 1
				}
				if end > totalPages {
					end = totalPages
				}
				for i := start; i <= end; i++ {
					result[i] = true
				}
			}
		} else {
			var page int
			if _, err := fmt.Sscanf(part, "%d", &page); err == nil {
				if page >= 1 && page <= totalPages {
					result[page] = true
				}
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// stripXMLTags removes XML tags from a string.
func stripXMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, ch := range s {
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			b.WriteRune(' ')
			continue
		}
		if !inTag {
			b.WriteRune(ch)
		}
	}
	return b.String()
}
