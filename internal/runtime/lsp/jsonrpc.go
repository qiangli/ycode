package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is a JSON-RPC 2.0 error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// conn manages the stdio connection to an LSP server process.
type conn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID atomic.Int64

	mu       sync.Mutex
	pending  map[int]chan *jsonRPCResponse
	closed   bool
	closeErr error
}

// startServer spawns an LSP server process and establishes a JSON-RPC connection.
func startServer(ctx context.Context, config ServerConfig, rootDir string) (*conn, error) {
	cmd := exec.CommandContext(ctx, config.Command, config.Args...)
	cmd.Dir = rootDir
	cmd.Stderr = os.Stderr // log server errors

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start %s: %w", config.Command, err)
	}

	c := &conn{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReaderSize(stdout, 64*1024),
		pending: make(map[int]chan *jsonRPCResponse),
	}

	// Read responses in a goroutine.
	go c.readLoop()

	return c, nil
}

// call sends a JSON-RPC request and waits for the response.
func (c *conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := int(c.nextID.Add(1))

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Create response channel before sending.
	ch := make(chan *jsonRPCResponse, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("connection closed")
	}
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	// Send the request with Content-Length header (LSP base protocol).
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.mu.Lock()
	_, writeErr := io.WriteString(c.stdin, header)
	if writeErr == nil {
		_, writeErr = c.stdin.Write(data)
	}
	c.mu.Unlock()
	if writeErr != nil {
		return nil, fmt.Errorf("write request: %w", writeErr)
	}

	// Wait for response.
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("LSP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// readLoop reads JSON-RPC responses from the server.
func (c *conn) readLoop() {
	for {
		// Read Content-Length header.
		contentLen, err := c.readHeader()
		if err != nil {
			if !c.closed {
				slog.Debug("LSP read header error", "error", err)
			}
			return
		}

		// Read the body.
		body := make([]byte, contentLen)
		if _, err := io.ReadFull(c.stdout, body); err != nil {
			if !c.closed {
				slog.Debug("LSP read body error", "error", err)
			}
			return
		}

		// Parse response.
		var resp jsonRPCResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			slog.Debug("LSP parse response error", "error", err)
			continue
		}

		// If it has an ID, deliver to the waiting caller.
		if resp.ID > 0 {
			c.mu.Lock()
			if ch, ok := c.pending[resp.ID]; ok {
				ch <- &resp
			}
			c.mu.Unlock()
		}
		// Notifications (no ID) are silently discarded for now.
	}
}

// readHeader reads the Content-Length header from LSP base protocol.
func (c *conn) readHeader() (int, error) {
	contentLen := 0
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			// Empty line marks end of header.
			if contentLen == 0 {
				return 0, fmt.Errorf("missing Content-Length header")
			}
			return contentLen, nil
		}
		if strings.HasPrefix(line, "Content-Length:") {
			valStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			n, err := strconv.Atoi(valStr)
			if err != nil {
				return 0, fmt.Errorf("parse Content-Length: %w", err)
			}
			contentLen = n
		}
	}
}

// close shuts down the connection and kills the server process.
func (c *conn) close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return c.closeErr
	}
	c.closed = true
	c.mu.Unlock()

	c.stdin.Close()
	c.closeErr = c.cmd.Process.Kill()
	c.cmd.Wait()
	return c.closeErr
}

// notify sends a JSON-RPC notification (no response expected).
func (c *conn) notify(method string, params any) error {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(data)
	return err
}

// fileURI converts a file path to a file:// URI.
func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return "file://" + abs
}

// uriToPath converts a file:// URI back to a file path.
func uriToPath(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Scheme == "file" {
		return u.Path
	}
	return uri
}
