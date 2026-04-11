package otel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RequestLoggerConfig configures the conversation request logger.
type RequestLoggerConfig struct {
	RetentionDays  int  // default 3
	LogToolDetails bool // include full tool input/output
}

// RequestLogger writes full conversation records to rotating JSONL files.
type RequestLogger struct {
	dir         string
	retention   time.Duration
	logToolDets bool

	mu          sync.Mutex
	currentFile *os.File
	currentDate string
}

// ConversationRecord is one JSONL line per API call, capturing the full
// request, response, tool calls, and cost for auditing and self-healing.
type ConversationRecord struct {
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"session_id"`
	TurnIndex int       `json:"turn_index"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`

	// Request.
	SystemPrompt string          `json:"system_prompt"`
	Messages     json.RawMessage `json:"messages"`
	ToolDefs     int             `json:"tool_defs"`
	MaxTokens    int             `json:"max_tokens"`
	Temperature  *float64        `json:"temperature,omitempty"`

	// Response.
	ResponseText    string        `json:"response_text"`
	ThinkingContent string        `json:"thinking_content,omitempty"`
	ToolCalls       []ToolCallLog `json:"tool_calls,omitempty"`
	StopReason      string        `json:"stop_reason"`

	// Usage & cost.
	TokensIn         int     `json:"tokens_in"`
	TokensOut        int     `json:"tokens_out"`
	CacheCreation    int     `json:"cache_creation"`
	CacheRead        int     `json:"cache_read"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	DurationMs       int64   `json:"duration_ms"`
	Success          bool    `json:"success"`
	Error            string  `json:"error,omitempty"`
}

// ToolCallLog captures full tool invocation details.
type ToolCallLog struct {
	Name       string          `json:"name"`
	Source     string          `json:"source,omitempty"`
	Input      json.RawMessage `json:"input"`
	Output     string          `json:"output"`
	Error      string          `json:"error,omitempty"`
	Success    bool            `json:"success"`
	DurationMs int64           `json:"duration_ms"`
}

// NewRequestLogger creates a conversation logger that writes to dir.
func NewRequestLogger(dir string, cfg RequestLoggerConfig) (*RequestLogger, error) {
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	retention := time.Duration(cfg.RetentionDays) * 24 * time.Hour
	if retention <= 0 {
		retention = 3 * 24 * time.Hour
	}
	return &RequestLogger{
		dir:         logDir,
		retention:   retention,
		logToolDets: cfg.LogToolDetails,
	}, nil
}

// Log writes a conversation record to the current day's JSONL file.
func (rl *RequestLogger) Log(record *ConversationRecord) error {
	if record == nil {
		return nil
	}

	// Strip tool details if disabled.
	if !rl.logToolDets {
		for i := range record.ToolCalls {
			record.ToolCalls[i].Input = nil
			record.ToolCalls[i].Output = ""
		}
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	data = append(data, '\n')

	rl.mu.Lock()
	defer rl.mu.Unlock()

	f, err := rl.fileForToday()
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

// Close closes the current log file.
func (rl *RequestLogger) Close() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.currentFile != nil {
		err := rl.currentFile.Close()
		rl.currentFile = nil
		return err
	}
	return nil
}

func (rl *RequestLogger) fileForToday() (*os.File, error) {
	today := time.Now().Format("2006-01-02")
	if rl.currentDate == today && rl.currentFile != nil {
		return rl.currentFile, nil
	}
	// Close previous file.
	if rl.currentFile != nil {
		rl.currentFile.Close()
	}
	filename := filepath.Join(rl.dir, fmt.Sprintf("conversations-%s.jsonl", today))
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	rl.currentFile = f
	rl.currentDate = today
	return f, nil
}
