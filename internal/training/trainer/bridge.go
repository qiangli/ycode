package trainer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// BridgeProtocol defines the IPC protocol between Go orchestrator and Python trainer.
// Communication is via JSONL over stdin/stdout.

// TrainRequest is sent from Go to Python.
type TrainRequest struct {
	Type         string          `json:"type"` // "train_step", "evaluate", "save_checkpoint"
	Trajectories json.RawMessage `json:"trajectories,omitempty"`
	Config       json.RawMessage `json:"config,omitempty"`
}

// TrainResponse is received from Python.
type TrainResponse struct {
	Type   string      `json:"type"` // "step_result", "eval_result", "error"
	Result *StepResult `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// BridgeWriter writes JSONL messages to a writer (stdin pipe to Python).
type BridgeWriter struct {
	w io.Writer
}

// NewBridgeWriter creates a bridge writer.
func NewBridgeWriter(w io.Writer) *BridgeWriter {
	return &BridgeWriter{w: w}
}

// Send writes a request as a JSONL line.
func (bw *BridgeWriter) Send(req *TrainRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')
	_, err = bw.w.Write(data)
	return err
}

// BridgeReader reads JSONL responses from a reader (stdout pipe from Python).
type BridgeReader struct {
	scanner *bufio.Scanner
}

// NewBridgeReader creates a bridge reader.
func NewBridgeReader(r io.Reader) *BridgeReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
	return &BridgeReader{scanner: scanner}
}

// Receive reads the next response.
func (br *BridgeReader) Receive() (*TrainResponse, error) {
	if !br.scanner.Scan() {
		if err := br.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	var resp TrainResponse
	if err := json.Unmarshal(br.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}
