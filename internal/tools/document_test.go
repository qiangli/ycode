package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/vfs"
)

func newTestVFS(t *testing.T, dir string) *vfs.VFS {
	t.Helper()
	v, err := vfs.New([]string{dir}, nil)
	if err != nil {
		t.Fatalf("create vfs: %v", err)
	}
	return v
}

func TestReadDocument_PDF(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Test that spec is registered and handler rejects non-PDF files.
	tmpDir := t.TempDir()
	v := newTestVFS(t, tmpDir)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterDocumentHandler(r, v)

	spec, ok := r.Get("read_document")
	if !ok {
		t.Fatal("read_document not registered")
	}

	// Missing file.
	_, err := spec.Handler(context.Background(), json.RawMessage(`{"file_path":"`+filepath.Join(tmpDir, "missing.pdf")+`"}`))
	if err == nil {
		t.Error("expected error for missing file")
	}

	// Empty path.
	_, err = spec.Handler(context.Background(), json.RawMessage(`{"file_path":""}`))
	if err == nil {
		t.Error("expected error for empty path")
	}

	// The PDF library requires a well-formed PDF with valid xref.
	// Instead of generating one, verify the handler dispatches correctly
	// by checking the error mentions "PDF" (not "unsupported").
	fakePDF := filepath.Join(tmpDir, "fake.pdf")
	if err := os.WriteFile(fakePDF, []byte("not a real pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = spec.Handler(context.Background(), json.RawMessage(`{"file_path":"`+fakePDF+`"}`))
	if err == nil {
		t.Error("expected error for invalid PDF")
	}
	if !strings.Contains(err.Error(), "PDF") {
		t.Errorf("expected PDF-related error, got: %v", err)
	}
}

func TestReadDocument_DOCX(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	// Create a minimal DOCX (ZIP with word/document.xml).
	docxPath := filepath.Join(tmpDir, "test.docx")
	createMinimalDOCX(t, docxPath, "Hello from DOCX")

	v := newTestVFS(t, tmpDir)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterDocumentHandler(r, v)

	spec, _ := r.Get("read_document")
	input := json.RawMessage(`{"file_path":"` + docxPath + `"}`)
	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("read_document DOCX failed: %v", err)
	}
	if !strings.Contains(result, "DOCX:") {
		t.Errorf("expected DOCX header in result, got: %s", result)
	}
}

func TestReadDocument_XLSX(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	// Use excelize to create a test XLSX.
	xlsxPath := filepath.Join(tmpDir, "test.xlsx")
	createMinimalXLSX(t, xlsxPath)

	v := newTestVFS(t, tmpDir)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterDocumentHandler(r, v)

	spec, _ := r.Get("read_document")
	input := json.RawMessage(`{"file_path":"` + xlsxPath + `"}`)
	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("read_document XLSX failed: %v", err)
	}
	if !strings.Contains(result, "XLSX:") {
		t.Errorf("expected XLSX header in result, got: %s", result)
	}
	if !strings.Contains(result, "test data") {
		t.Errorf("expected 'test data' in result, got: %s", result)
	}
}

func TestReadDocument_CSV(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "test.csv")
	if err := os.WriteFile(csvPath, []byte("name,age\nAlice,30\nBob,25\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := newTestVFS(t, tmpDir)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterDocumentHandler(r, v)

	spec, _ := r.Get("read_document")
	input := json.RawMessage(`{"file_path":"` + csvPath + `"}`)
	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("read_document CSV failed: %v", err)
	}
	if !strings.Contains(result, "Alice") {
		t.Errorf("expected CSV content in result, got: %s", result)
	}
}

func TestReadDocument_PPTX(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	pptxPath := filepath.Join(tmpDir, "test.pptx")
	createMinimalPPTX(t, pptxPath)

	v := newTestVFS(t, tmpDir)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterDocumentHandler(r, v)

	spec, _ := r.Get("read_document")
	input := json.RawMessage(`{"file_path":"` + pptxPath + `"}`)
	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("read_document PPTX failed: %v", err)
	}
	if !strings.Contains(result, "PPTX:") {
		t.Errorf("expected PPTX header in result, got: %s", result)
	}
}

func TestReadDocument_Unsupported(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	txtPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(txtPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := newTestVFS(t, tmpDir)
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterDocumentHandler(r, v)

	spec, _ := r.Get("read_document")
	_, err := spec.Handler(context.Background(), json.RawMessage(`{"file_path":"`+txtPath+`"}`))
	if err == nil {
		t.Error("expected error for unsupported file type")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected 'unsupported' in error, got: %v", err)
	}
}

func TestReadDocument_PageRange(t *testing.T) {
	tests := []struct {
		input string
		total int
		want  map[int]bool
	}{
		{"1-3", 10, map[int]bool{1: true, 2: true, 3: true}},
		{"5", 10, map[int]bool{5: true}},
		{"1,3,5", 10, map[int]bool{1: true, 3: true, 5: true}},
		{"", 10, nil},
		{"1-100", 5, map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}},
	}

	for _, tt := range tests {
		got := parsePageRange(tt.input, tt.total)
		if tt.want == nil {
			if got != nil {
				t.Errorf("parsePageRange(%q, %d): expected nil, got %v", tt.input, tt.total, got)
			}
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("parsePageRange(%q, %d): expected %d pages, got %d", tt.input, tt.total, len(tt.want), len(got))
		}
	}
}

// --- Helpers ---

func createMinimalDOCX(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)

	// Add minimal [Content_Types].xml
	ct, _ := w.Create("[Content_Types].xml")
	ct.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`))

	// Add _rels/.rels
	rels, _ := w.Create("_rels/.rels")
	rels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`))

	// Add word/_rels/document.xml.rels (required by docx library)
	docRels, _ := w.Create("word/_rels/document.xml.rels")
	docRels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
</Relationships>`))

	// Add word/document.xml
	doc, _ := w.Create("word/document.xml")
	doc.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body>
</w:document>`))

	w.Close()
	f.Close()
}

func createMinimalXLSX(t *testing.T, path string) {
	t.Helper()
	// Use excelize to create a proper XLSX.
	// Import it from the test's perspective.
	// Since we're in the tools package and excelize is already imported in document.go,
	// we create it manually with a minimal ZIP approach.
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)

	ct, _ := w.Create("[Content_Types].xml")
	ct.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>
</Types>`))

	rels, _ := w.Create("_rels/.rels")
	rels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`))

	wbRels, _ := w.Create("xl/_rels/workbook.xml.rels")
	wbRels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>
</Relationships>`))

	wb, _ := w.Create("xl/workbook.xml")
	wb.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets>
</workbook>`))

	ss, _ := w.Create("xl/sharedStrings.xml")
	ss.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="2" uniqueCount="2">
<si><t>header</t></si>
<si><t>test data</t></si>
</sst>`))

	sheet, _ := w.Create("xl/worksheets/sheet1.xml")
	sheet.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c></row>
<row r="2"><c r="A2" t="s"><v>1</v></c></row>
</sheetData>
</worksheet>`))

	w.Close()
	f.Close()
}

func createMinimalPPTX(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)

	ct, _ := w.Create("[Content_Types].xml")
	ct.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="xml" ContentType="application/xml"/>
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
</Types>`))

	slide, _ := w.Create("ppt/slides/slide1.xml")
	slide.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
<p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Slide text content</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld>
</p:sld>`))

	w.Close()
	f.Close()
}
