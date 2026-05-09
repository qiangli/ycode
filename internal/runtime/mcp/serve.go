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
// requests to the given handler. This is used by `ycode mcp serve` to
// expose ycode's tools as an MCP server.
//
// Wire framing: newline-delimited JSON, per the MCP stdio transport
// specification. Each request and each response is a single JSON
// document terminated by '\n'; messages MUST NOT contain embedded
// newlines. (See modelcontextprotocol.io specification basic/transports
// stdio.) An earlier implementation here used LSP-style Content-Length
// framing, which made every standards-compliant client (Claude Code,
// Cursor, the reference TS SDK) hang for 30 seconds and time out —
// the bug we're fixing now.
func RunServer(ctx context.Context, handler ServerHandler) error {
	return runServerOn(ctx, handler, os.Stdin, os.Stdout)
}

// runServerOn is the testable core: same loop, but read/write streams
// are injected so unit tests can drive both sides via in-memory pipes.
func runServerOn(ctx context.Context, handler ServerHandler, in io.Reader, out io.Writer) error {
	server := NewServer(handler)
	scanner := bufio.NewScanner(in)
	// MCP messages can be much larger than the default 64 KiB scanner
	// buffer (e.g. a tools/list response carrying many schemas), so
	// raise the cap to 1 MiB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		// Tolerate blank lines between messages — the spec is silent
		// on them, but real clients sometimes flush an empty line
		// after a notification.
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = writeJSON(out, JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: -32700, Message: "Parse error"},
			})
			continue
		}

		resp, err := server.HandleRequest(ctx, &req)
		if err != nil {
			_ = writeJSON(out, JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &JSONRPCError{Code: -32603, Message: err.Error()},
			})
			continue
		}

		if err := writeJSON(out, resp); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return nil
}

// writeJSON encodes v as a single JSON line terminated by '\n'. The MCP
// stdio spec forbids embedded newlines, and json.Marshal already emits
// compact output without internal \n, so a trailing newline is the only
// framing needed.
func writeJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err = w.Write([]byte{'\n'})
	return err
}
