package adapters

import (
	"bytes"
	"io"
)

// jsonReader wraps a byte slice as an io.Reader for HTTP request bodies.
func jsonReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}
