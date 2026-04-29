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
	// Write with Content-Length header per LSP/MCP protocol.
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := io.WriteString(t.stdin, header); err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("write header: %w", err)
	}
	if _, err := t.stdin.Write(data); err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("write body: %w", err)
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

// readResponse reads a JSON-RPC response from stdout.
func (t *StdioTransport) readResponse(resp *JSONRPCResponse) error {
	// Read Content-Length header.
	var contentLen int
	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read header: %w", err)
		}
		line = line[:len(line)-1] // trim \n
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if line == "" {
			break // end of headers
		}
		if n, err := fmt.Sscanf(line, "Content-Length: %d", &contentLen); err == nil && n == 1 {
			continue
		}
	}

	if contentLen <= 0 {
		return fmt.Errorf("invalid content length: %d", contentLen)
	}

	body := make([]byte, contentLen)
	if _, err := io.ReadFull(t.reader, body); err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	return json.Unmarshal(body, resp)
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

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := io.WriteString(t.stdin, header); err != nil {
		return fmt.Errorf("write notification header: %w", err)
	}
	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("write notification body: %w", err)
	}
	return nil
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
