package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// RunServer runs the MCP server protocol on stdin/stdout, dispatching
// requests to the given handler. This is used by `ycode mcp serve`
// to expose ycode's tools as an MCP server.
func RunServer(ctx context.Context, handler ServerHandler) error {
	server := NewServer(handler)
	reader := bufio.NewReader(os.Stdin)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Read Content-Length header.
		var contentLen int
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return nil // clean shutdown
				}
				return fmt.Errorf("read header: %w", err)
			}
			line = line[:len(line)-1] // trim \n
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if line == "" {
				break
			}
			if n, err := fmt.Sscanf(line, "Content-Length: %d", &contentLen); err == nil && n == 1 {
				continue
			}
		}

		if contentLen <= 0 {
			continue
		}

		body := make([]byte, contentLen)
		if _, err := io.ReadFull(reader, body); err != nil {
			return fmt.Errorf("read body: %w", err)
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(os.Stdout, 0, -32700, "Parse error")
			continue
		}

		resp, err := server.HandleRequest(ctx, &req)
		if err != nil {
			writeError(os.Stdout, req.ID, -32603, err.Error())
			continue
		}

		data, err := json.Marshal(resp)
		if err != nil {
			writeError(os.Stdout, req.ID, -32603, "marshal response: "+err.Error())
			continue
		}

		header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
		if _, err := io.WriteString(os.Stdout, header); err != nil {
			return fmt.Errorf("write response header: %w", err)
		}
		if _, err := os.Stdout.Write(data); err != nil {
			return fmt.Errorf("write response body: %w", err)
		}
	}
}

func writeError(w io.Writer, id int64, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	}
	data, _ := json.Marshal(resp)
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	io.WriteString(w, header)
	w.Write(data)
}
