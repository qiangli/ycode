package task

import (
	"fmt"
	"regexp"
	"strings"
)

// GSM8K implements the GSM8K math word problem task.
type GSM8K struct {
	examples []Example
}

// NewGSM8K creates a GSM8K task with sample problems.
func NewGSM8K() *GSM8K {
	return &GSM8K{
		examples: []Example{
			{ID: "gsm8k-001", Prompt: "Janet's ducks lay 16 eggs per day. She eats three for breakfast every morning and bakes muffins for her friends every day with four. She sells every duck egg at the farmers' market daily for $2. How much in dollars does she make every day at the farmers' market?", Expected: "18"},
			{ID: "gsm8k-002", Prompt: "A robe takes 2 bolts of blue fiber and half that much white fiber. How many bolts in total does it take?", Expected: "3"},
			{ID: "gsm8k-003", Prompt: "Josh decides to try flipping a house. He buys a house for $80,000 and puts $50,000 in repairs. This increased the value of the house by 150%. How much profit did he make?", Expected: "70000"},
			{ID: "gsm8k-004", Prompt: "James writes a 3-page letter to 2 different friends twice a week. How many pages does he write a year?", Expected: "624"},
			{ID: "gsm8k-005", Prompt: "Every day, Wendi feeds each of her chickens three cups of mixed chicken feed, containing seeds, mealworms and vegetables to help keep them healthy. She gives the chickens their feed in three separate meals. In the morning, she gives her flock of chickens 15 cups of feed. In the afternoon, she gives her chickens another 25 cups of feed. If the carrying capacity of each meal is 15 cups, how many cups of feed does she need to give her chickens in the final meal of the day?", Expected: "20"},
		},
	}
}

func (g *GSM8K) Name() string { return "gsm8k" }
func (g *GSM8K) Len() int     { return len(g.examples) }

func (g *GSM8K) GetExample(index int) (*Example, error) {
	if index < 0 || index >= len(g.examples) {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, len(g.examples))
	}
	e := g.examples[index]
	return &e, nil
}

// numberPattern matches a number (possibly negative, possibly with commas).
var numberPattern = regexp.MustCompile(`-?[\d,]+(?:\.\d+)?`)

func (g *GSM8K) Evaluate(example *Example, completion string) (float64, error) {
	// Extract the final number from the completion.
	matches := numberPattern.FindAllString(completion, -1)
	if len(matches) == 0 {
		return 0.0, nil
	}
	// Use the last number found.
	answer := strings.ReplaceAll(matches[len(matches)-1], ",", "")
	expected := strings.TrimSpace(example.Expected)
	if answer == expected {
		return 1.0, nil
	}
	return 0.0, nil
}
