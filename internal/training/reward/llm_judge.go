package reward

import (
	"context"
	"fmt"
	"strings"
)

// LLMJudgeConfig configures the LLM judge reward.
type LLMJudgeConfig struct {
	// JudgeFunc is called to ask an LLM to score the response.
	// It receives the prompt template with question+expected+actual filled in,
	// and should return the raw LLM response.
	JudgeFunc func(ctx context.Context, prompt string) (string, error)
}

// LLMJudgeReward uses an LLM to evaluate answer correctness.
type LLMJudgeReward struct {
	config   LLMJudgeConfig
	question string
	expected string
}

// NewLLMJudgeReward creates an LLM-as-judge reward scorer.
func NewLLMJudgeReward(question, expected string, cfg LLMJudgeConfig) *LLMJudgeReward {
	return &LLMJudgeReward{
		config:   cfg,
		question: question,
		expected: expected,
	}
}

const judgePromptTemplate = `You are evaluating an AI assistant's answer.

Question: %s
Expected answer: %s
Actual answer: %s

Rate the actual answer's correctness on a scale of 0.0 to 1.0:
- 1.0 = completely correct
- 0.5 = partially correct (right direction but missing details)
- 0.0 = incorrect

Reply with ONLY a number between 0.0 and 1.0.`

func (l *LLMJudgeReward) Score(ctx context.Context, result *AgentResult) (float64, error) {
	if l.config.JudgeFunc == nil {
		return 0.0, fmt.Errorf("JudgeFunc not configured")
	}

	// Extract the last assistant response.
	var answer string
	for i := len(result.Messages) - 1; i >= 0; i-- {
		if result.Messages[i].Role == "assistant" && result.Messages[i].Content != "" {
			answer = result.Messages[i].Content
			break
		}
	}
	if answer == "" {
		return 0.0, nil
	}

	prompt := fmt.Sprintf(judgePromptTemplate, l.question, l.expected, answer)
	response, err := l.config.JudgeFunc(ctx, prompt)
	if err != nil {
		return 0.0, fmt.Errorf("judge LLM call: %w", err)
	}

	return parseScore(strings.TrimSpace(response)), nil
}

// parseScore extracts a float from the judge response.
func parseScore(s string) float64 {
	var score float64
	_, err := fmt.Sscanf(s, "%f", &score)
	if err != nil || score < 0 {
		return 0.0
	}
	if score > 1.0 {
		return 1.0
	}
	return score
}
