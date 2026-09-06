package ycode_test

import (
	"context"
	"log"

	ycode "github.com/qiangli/ycode/pkg/ycode"
)

// ExampleHarness shows the YAML-native embedding flow. Applications normally
// forward each durable Event to their own transport or presentation layer.
func ExampleHarness() {
	if err := ycode.Validate("agent.yaml"); err != nil {
		log.Fatal(err)
	}
	harness, err := ycode.Load("agent.yaml")
	if err != nil {
		log.Fatal(err)
	}
	defer harness.Close()
	events, err := harness.Run(context.Background(), ycode.RunRequest{
		SessionID: "session-1", RunID: "turn-1", TriggerRef: "interactive-input",
		FrontendRef: "embed", Principal: "local-user", IdempotencyKey: "request-1",
		Body: []byte(`{"request":"explain this repository"}`),
	})
	if err != nil {
		log.Fatal(err)
	}
	for event := range events {
		_ = event
	}
}

var (
	_ func(string) error                                                                     = ycode.Validate
	_ func(string, ...ycode.LoadOption) (*ycode.Harness, error)                              = ycode.Load
	_ func(*ycode.Harness, context.Context, ycode.RunRequest) (<-chan ycode.Event, error)    = (*ycode.Harness).Run
	_ func(*ycode.Harness, context.Context, ycode.ResumeRequest) (<-chan ycode.Event, error) = (*ycode.Harness).Resume
	_ func(*ycode.Harness, context.Context, ycode.ForkRequest) (<-chan ycode.Event, error)   = (*ycode.Harness).Fork
	_ func(*ycode.Harness, string) ([]byte, error)                                           = (*ycode.Harness).Payload
)
