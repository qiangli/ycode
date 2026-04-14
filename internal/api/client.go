package api

import "context"

// ProviderKind identifies the API provider.
type ProviderKind string

const (
	ProviderAnthropic ProviderKind = "anthropic"
	ProviderOpenAI    ProviderKind = "openai"
	ProviderGemini    ProviderKind = "gemini"
)

// Provider is the interface that all API providers must implement.
type Provider interface {
	// Send sends a request and returns a channel of stream events.
	// The channel is closed when the stream ends or an error occurs.
	// The returned error channel will receive at most one error.
	Send(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error)

	// Kind returns the provider type.
	Kind() ProviderKind
}
