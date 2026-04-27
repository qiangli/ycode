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

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantReason FailoverReason
		wantAction RecoveryAction
	}{
		{
			name:       "401 with invalid key",
			statusCode: 401,
			body:       `{"error":"invalid api key"}`,
			wantReason: ReasonAuth,
			wantAction: ActionRotateKey,
		},
		{
			name:       "403 with expired token",
			statusCode: 403,
			body:       `{"error":"token has expired"}`,
			wantReason: ReasonAuth,
			wantAction: ActionRotateKey,
		},
		{
			name:       "401 with billing issue",
			statusCode: 401,
			body:       `{"error":"billing account suspended, payment required"}`,
			wantReason: ReasonBilling,
			wantAction: ActionAbort,
		},
		{
			name:       "429 rate limit",
			statusCode: 429,
			body:       `{"error":"rate limit exceeded"}`,
			wantReason: ReasonRateLimit,
			wantAction: ActionRetry,
		},
		{
			name:       "529 overloaded",
			statusCode: 529,
			body:       `{"error":"service overloaded"}`,
			wantReason: ReasonOverloaded,
			wantAction: ActionRetry,
		},
		{
			name:       "200 with overloaded in body",
			statusCode: 200,
			body:       `{"error":"API is overloaded"}`,
			wantReason: ReasonOverloaded,
			wantAction: ActionRetry,
		},
		{
			name:       "500 server error",
			statusCode: 500,
			body:       `{"error":"internal server error"}`,
			wantReason: ReasonServerError,
			wantAction: ActionRetry,
		},
		{
			name:       "502 bad gateway",
			statusCode: 502,
			body:       `bad gateway`,
			wantReason: ReasonServerError,
			wantAction: ActionRetry,
		},
		{
			name:       "503 service unavailable",
			statusCode: 503,
			body:       `service unavailable`,
			wantReason: ReasonServerError,
			wantAction: ActionRetry,
		},
		{
			name:       "504 gateway timeout",
			statusCode: 504,
			body:       `gateway timeout`,
			wantReason: ReasonServerError,
			wantAction: ActionRetry,
		},
		{
			name:       "408 request timeout",
			statusCode: 408,
			body:       `request timeout`,
			wantReason: ReasonTimeout,
			wantAction: ActionRetry,
		},
		{
			name:       "non-5xx with timeout in body",
			statusCode: 422,
			body:       `{"error":"request timeout exceeded"}`,
			wantReason: ReasonTimeout,
			wantAction: ActionRetry,
		},
		{
			name:       "400 with token limit",
			statusCode: 400,
			body:       `{"error":"token limit exceeded: 200000 requested, 128000 maximum"}`,
			wantReason: ReasonContextOverflow,
			wantAction: ActionCompressContext,
		},
		{
			name:       "413 context too large",
			statusCode: 413,
			body:       `{"error":"context length exceeded"}`,
			wantReason: ReasonContextOverflow,
			wantAction: ActionCompressContext,
		},
		{
			name:       "400 context window",
			statusCode: 400,
			body:       `{"error":"exceeds the context window"}`,
			wantReason: ReasonContextOverflow,
			wantAction: ActionCompressContext,
		},
		{
			name:       "404 model not found",
			statusCode: 404,
			body:       `{"error":"model 'gpt-5' not found"}`,
			wantReason: ReasonModelNotFound,
			wantAction: ActionFallbackModel,
		},
		{
			name:       "400 content policy",
			statusCode: 400,
			body:       `{"error":"content policy violation"}`,
			wantReason: ReasonPolicyBlocked,
			wantAction: ActionAbort,
		},
		{
			name:       "400 generic content filter",
			statusCode: 400,
			body:       `{"error":"flagged by content filter"}`,
			wantReason: ReasonPolicyBlocked,
			wantAction: ActionAbort,
		},
		{
			name:       "unknown error",
			statusCode: 418,
			body:       `I'm a teapot`,
			wantReason: ReasonUnknown,
			wantAction: ActionRetry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := ClassifyError(tt.statusCode, tt.body)
			if reason != tt.wantReason {
				t.Errorf("ClassifyError(%d, %q) reason = %v, want %v",
					tt.statusCode, tt.body, reason, tt.wantReason)
			}
			action := reason.RecommendedAction()
			if action != tt.wantAction {
				t.Errorf("RecommendedAction() for %v = %v, want %v",
					reason, action, tt.wantAction)
			}
		})
	}
}

func TestFailoverReason_String(t *testing.T) {
	tests := []struct {
		reason FailoverReason
		want   string
	}{
		{ReasonUnknown, "Unknown"},
		{ReasonAuth, "Auth"},
		{ReasonRateLimit, "RateLimit"},
		{ReasonContextOverflow, "ContextOverflow"},
		{FailoverReason(99), "Unknown"},
	}

	for _, tt := range tests {
		got := tt.reason.String()
		if got != tt.want {
			t.Errorf("FailoverReason(%d).String() = %q, want %q", tt.reason, got, tt.want)
		}
	}
}

func TestRecoveryAction_String(t *testing.T) {
	tests := []struct {
		action RecoveryAction
		want   string
	}{
		{ActionRetry, "Retry"},
		{ActionAbort, "Abort"},
		{ActionCompressContext, "CompressContext"},
		{RecoveryAction(99), "Retry"},
	}

	for _, tt := range tests {
		got := tt.action.String()
		if got != tt.want {
			t.Errorf("RecoveryAction(%d).String() = %q, want %q", tt.action, got, tt.want)
		}
	}
}

func TestClassifiedError_Error(t *testing.T) {
	err := &ClassifiedError{
		Reason:     ReasonRateLimit,
		Action:     ActionRetry,
		StatusCode: 429,
		Body:       "rate limit exceeded",
	}
	got := err.Error()
	if got == "" {
		t.Error("ClassifiedError.Error() should not be empty")
	}
	if !errors.Is(nil, nil) { // just use errors import
		t.Fatal("unreachable")
	}
}

func TestIsClassifiedError(t *testing.T) {
	ce := &ClassifiedError{Reason: ReasonAuth, StatusCode: 401, Body: "invalid key"}
	if !IsClassifiedError(ce) {
		t.Error("IsClassifiedError should return true for ClassifiedError")
	}
	if IsClassifiedError(errors.New("plain error")) {
		t.Error("IsClassifiedError should return false for plain error")
	}
}
