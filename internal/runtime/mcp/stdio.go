package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// StdioTransport communicates with an MCP server via stdin/stdout.
type StdioTransport struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	reader    *bufio.Reader
	stderrBuf *bytes.Buffer

	mu     sync.Mutex
	nextID atomic.Int64
}

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// NewStdioTransport creates a stdio transport for the given command.
func NewStdioTransport(command string, args []string, env map[string]string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)

	// Inherit the current environment, then overlay custom env vars.
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Capture stderr for diagnostics.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	return &StdioTransport{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		reader:    bufio.NewReader(stdout),
		stderrBuf: &stderrBuf,
	}, nil
}

// Start launches the MCP server process.
func (t *StdioTransport) Start() error {
	return t.cmd.Start()
}

// Call sends a JSON-RPC request and returns the response.
func (t *StdioTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.nextID.Add(1)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	t.mu.Lock()
	if err := writeFrame(t.stdin, data); err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("write request: %w", err)
	}
	t.mu.Unlock()

	// Read response.
	var resp JSONRPCResponse
	if err := t.readResponse(&resp); err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	return resp.Result, nil
}

// readResponse reads one newline-delimited JSON-RPC response. The MCP
// stdio transport frames each message as a single line terminated by
// `\n`; embedded newlines are forbidden.
func (t *StdioTransport) readResponse(resp *JSONRPCResponse) error {
	for {
		line, err := t.reader.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}
		// Trim trailing \r\n / \n.
		line = trimNewline(line)
		if len(line) == 0 {
			continue // tolerate blank framing lines
		}
		return json.Unmarshal(line, resp)
	}
}

// Notify sends a JSON-RPC notification (no response expected).
func (t *StdioTransport) Notify(ctx context.Context, method string, params any) error {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if err := writeFrame(t.stdin, data); err != nil {
		return fmt.Errorf("write notification: %w", err)
	}
	return nil
}

// writeFrame emits one MCP stdio frame: the JSON message followed by a
// single '\n'. Used by both Call and Notify to keep framing in one
// place.
func writeFrame(w io.Writer, data []byte) error {
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err := w.Write([]byte{'\n'})
	return err
}

// trimNewline strips a trailing '\n' (and optionally '\r' before it)
// from line. Returns line unchanged if no terminator is present.
func trimNewline(line []byte) []byte {
	if n := len(line); n > 0 && line[n-1] == '\n' {
		line = line[:n-1]
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
	}
	return line
}

// Stderr returns any captured stderr output from the server process.
func (t *StdioTransport) Stderr() string {
	if t.stderrBuf == nil {
		return ""
	}
	return t.stderrBuf.String()
}

// Close shuts down the transport gracefully. It closes stdin to signal
// the server, waits briefly for exit, then kills if needed.
func (t *StdioTransport) Close() error {
	t.stdin.Close()

	if t.cmd.Process != nil {
		// Give the server 2 seconds to exit gracefully.
		done := make(chan error, 1)
		go func() { done <- t.cmd.Wait() }()

		select {
		case <-done:
			// Process exited cleanly.
		case <-time.After(2 * time.Second):
			_ = t.cmd.Process.Kill()
			<-done
		}
	}

	t.stdout.Close()
	return nil
}
