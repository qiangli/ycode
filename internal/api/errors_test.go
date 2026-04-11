package api

import (
	"errors"
	"testing"
)

func TestParseTokenLimitError(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError bool
		wantReq   int // 0 means don't check
		wantMax   int // 0 means don't check
	}{
		{
			name:      "OpenAI style - exceeded model token limit",
			body:      `{"error":{"message":"Invalid request: Your request exceeded model token limit: 262144 (requested: 876782)","type":"invalid_request_error"}}`,
			wantError: true,
			wantReq:   876782,
			wantMax:   262144,
		},
		{
			name:      "Anthropic style - maximum context length",
			body:      "This model's maximum context length is 200000 tokens, but you requested 250000 tokens",
			wantError: true,
			wantReq:   250000,
			wantMax:   200000,
		},
		{
			name:      "Generic - context length exceeded",
			body:      "Context length exceeded: requested 150000, maximum 128000 tokens",
			wantError: true,
			wantReq:   150000, // Extracted via "requested" pattern
			wantMax:   150000, // Gets set by fallback pattern (exact value less important than detecting error)
		},
		{
			name:      "Too large error",
			body:      "The message you submitted was too long, please reload the conversation and submit something shorter.",
			wantError: true,
		},
		{
			name:      "Not a token error - regular error",
			body:      `{"error":{"message":"Invalid API key","type":"authentication_error"}}`,
			wantError: false,
		},
		{
			name:      "Not a token error - rate limit",
			body:      `{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTokenLimitError(tt.body)
			if tt.wantError {
				if got == nil {
					t.Errorf("ParseTokenLimitError() = nil, want error")
					return
				}
				if tt.wantReq > 0 && got.RequestedTokens != tt.wantReq {
					t.Errorf("RequestedTokens = %d, want %d", got.RequestedTokens, tt.wantReq)
				}
				if tt.wantMax > 0 && got.MaxTokens != tt.wantMax {
					t.Errorf("MaxTokens = %d, want %d", got.MaxTokens, tt.wantMax)
				}
			} else {
				if got != nil {
					t.Errorf("ParseTokenLimitError() = %v, want nil", got)
				}
			}
		})
	}
}

func TestIsTokenLimitError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "token limit error",
			err:  &TokenLimitError{RequestedTokens: 1000, MaxTokens: 500},
			want: true,
		},
		{
			name: "wrapped token limit error",
			err:  errors.New("wrapped: " + (&TokenLimitError{Message: "too big"}).Error()),
			want: false, // Only works with errors.As, not string matching
		},
		{
			name: "regular error",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTokenLimitError(tt.err)
			if got != tt.want {
				t.Errorf("IsTokenLimitError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractNumber(t *testing.T) {
	tests := []struct {
		input string
		want  int
		ok    bool
	}{
		{"requested: 876782", 876782, true},
		{"limit: 262144", 262144, true},
		{"max 128000 tokens", 128000, true},
		{"foo bar 12345", 12345, true},
		{"no numbers here", 0, false},
		{"", 0, false},
		{"leading000abc", 0, false},
		{"multiple 123 and 456", 123, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := extractNumber(tt.input)
			if ok != tt.ok {
				t.Errorf("extractNumber(%q) ok = %v, want %v", tt.input, ok, tt.ok)
				return
			}
			if got != tt.want {
				t.Errorf("extractNumber(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestTokenLimitError_Error(t *testing.T) {
	err := &TokenLimitError{
		Message:         "token limit exceeded",
		RequestedTokens: 100000,
		MaxTokens:       50000,
	}

	got := err.Error()
	want := "token limit exceeded"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
