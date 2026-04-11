package fileops

import (
	"io"
	"os"
)

const binaryCheckSize = 8 * 1024 // 8KB

// IsBinaryFile checks if a file appears to be binary by reading the first 8KB
// and looking for NUL bytes. Returns true if the file is likely binary.
// Returns false for empty files.
func IsBinaryFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, binaryCheckSize)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	if n == 0 {
		return false, nil
	}

	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true, nil
		}
	}
	return false, nil
}
