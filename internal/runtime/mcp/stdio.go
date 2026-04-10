package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// StdioTransport communicates with an MCP server via stdin/stdout.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader

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

	if len(env) > 0 {
		for k, v := range env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		reader: bufio.NewReader(stdout),
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

// Close shuts down the transport and kills the process.
func (t *StdioTransport) Close() error {
	t.stdin.Close()
	t.stdout.Close()
	if t.cmd.Process != nil {
		return t.cmd.Process.Kill()
	}
	return nil
}
