package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/api"
)

// Judge uses an LLM to evaluate open-ended agent responses.
// This is the LLM-as-judge pattern — a separate model scores the
// quality of the agent's output on a 0.0-1.0 scale.
type Judge struct {
	provider api.Provider
	model    string
}

// NewJudge creates a judge backed by the given provider.
// For local evaluation, use the Ollama provider. For higher quality,
// use a frontier model.
func NewJudge(provider api.Provider, model string) *Judge {
	return &Judge{provider: provider, model: model}
}

// JudgeResult holds the judge's assessment.
type JudgeResult struct {
	Score       float64 `json:"score"`       // 0.0-1.0
	Explanation string  `json:"explanation"` // Why this score
}

// ScoreResponse asks the judge to evaluate an agent's response against criteria.
//
// The rubric should describe what a perfect response looks like.
// The judge returns a score from 0.0 to 1.0 with an explanation.
func (j *Judge) ScoreResponse(ctx context.Context, prompt, response, rubric string) (*JudgeResult, error) {
	judgePrompt := fmt.Sprintf(`You are an evaluation judge. Score the following agent response on a scale of 0.0 to 1.0.

## Task Given to Agent
%s

## Agent's Response
%s

## Scoring Rubric
%s

## Instructions
Return a JSON object with exactly two fields:
- "score": a float between 0.0 and 1.0
- "explanation": a brief explanation of your score

Return ONLY the JSON object, nothing else.`, prompt, response, rubric)

	req := &api.Request{
		Model:     j.model,
		MaxTokens: 500,
		Messages: []api.Message{
			{
				Role: api.RoleUser,
				Content: []api.ContentBlock{
					{Type: api.ContentTypeText, Text: judgePrompt},
				},
			},
		},
		Stream: false,
	}

	events, errs := j.provider.Send(ctx, req)

	var responseText string
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				goto done
			}
			if evt.ContentBlock != nil && evt.ContentBlock.Text != "" {
				responseText += evt.ContentBlock.Text
			}
		case err, ok := <-errs:
			if !ok {
				goto done
			}
			if err != nil {
				return nil, fmt.Errorf("judge provider error: %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

done:
	// Parse JSON from response.
	responseText = strings.TrimSpace(responseText)
	// Strip markdown code fences if present.
	responseText = strings.TrimPrefix(responseText, "```json")
	responseText = strings.TrimPrefix(responseText, "```")
	responseText = strings.TrimSuffix(responseText, "```")
	responseText = strings.TrimSpace(responseText)

	var result JudgeResult
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		return &JudgeResult{
			Score:       0.5,
			Explanation: fmt.Sprintf("failed to parse judge response: %s", responseText),
		}, nil
	}

	// Clamp score.
	if result.Score < 0 {
		result.Score = 0
	}
	if result.Score > 1 {
		result.Score = 1
	}

	return &result, nil
}

// JudgeAssertion is an Assertion that uses LLM-as-judge to score responses.
type JudgeAssertion struct {
	Judge    *Judge
	Rubric   string
	MinScore float64 // Minimum acceptable score (default 0.7)
}

// Check evaluates the response using the LLM judge.
func (a *JudgeAssertion) Check(result *RunResult) error {
	if a.Judge == nil {
		return fmt.Errorf("judge not configured")
	}

	ctx := context.Background()
	jr, err := a.Judge.ScoreResponse(ctx, "", result.Response, a.Rubric)
	if err != nil {
		return fmt.Errorf("judge error: %w", err)
	}

	minScore := a.MinScore
	if minScore == 0 {
		minScore = 0.7
	}

	if jr.Score < minScore {
		return fmt.Errorf("judge score %.2f < %.2f: %s", jr.Score, minScore, jr.Explanation)
	}
	return nil
}

func (a *JudgeAssertion) String() string {
	return fmt.Sprintf("JudgeAssertion(min=%.2f)", a.MinScore)
}
