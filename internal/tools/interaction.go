package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// UserPrompter handles user interaction during tool execution.
type UserPrompter interface {
	AskQuestion(ctx context.Context, question string, choices []string) (string, error)
	SendMessage(ctx context.Context, message string) error
}

// RegisterInteractionHandlers registers AskUserQuestion and SendUserMessage.
func RegisterInteractionHandlers(r *Registry, prompter UserPrompter) {
	if spec, ok := r.Get("AskUserQuestion"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Question string   `json:"question"`
				Choices  []string `json:"choices,omitempty"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse AskUserQuestion input: %w", err)
			}
			if prompter == nil {
				return "", fmt.Errorf("no user prompter available")
			}
			return prompter.AskQuestion(ctx, params.Question, params.Choices)
		}
	}

	if spec, ok := r.Get("SendUserMessage"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse SendUserMessage input: %w", err)
			}
			if params.Message == "" {
				return "", fmt.Errorf("message is required")
			}
			if prompter == nil {
				return "", fmt.Errorf("no user prompter available")
			}
			if err := prompter.SendMessage(ctx, params.Message); err != nil {
				return "", err
			}
			return "Message sent to user.", nil
		}
	}
}
