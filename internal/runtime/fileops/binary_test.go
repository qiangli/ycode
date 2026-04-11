package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsBinaryFile(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		content []byte
		wantBin bool
		wantErr bool
	}{
		{
			name:    "text file",
			content: []byte("hello world\nthis is a text file\n"),
			wantBin: false,
		},
		{
			name:    "file with NUL byte",
			content: []byte("hello\x00world"),
			wantBin: true,
		},
		{
			name:    "empty file",
			content: []byte{},
			wantBin: false,
		},
		{
			name:    "binary header",
			content: append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}, []byte("PNG data")...),
			wantBin: true,
		},
		{
			name:    "utf8 text",
			content: []byte("日本語テスト\nこんにちは\n"),
			wantBin: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name)
			if err := os.WriteFile(path, tt.content, 0o644); err != nil {
				t.Fatal(err)
			}

			got, err := IsBinaryFile(path)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsBinaryFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantBin {
				t.Errorf("IsBinaryFile() = %v, want %v", got, tt.wantBin)
			}
		})
	}
}

func TestIsBinaryFile_NonExistent(t *testing.T) {
	_, err := IsBinaryFile("/nonexistent/file/path")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}
