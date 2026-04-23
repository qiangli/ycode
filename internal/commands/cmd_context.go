package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/prompt"
)

// contextHandler returns a handler that displays context window usage.
func contextHandler(deps *RuntimeDeps) HandlerFunc {
	return func(ctx context.Context, args string) (string, error) {
		workDir := deps.WorkDir
		if workDir == "" {
			return "", fmt.Errorf("working directory not set")
		}

		var b strings.Builder

		// Instruction files.
		files := prompt.DiscoverInstructionFiles(workDir, workDir)

		// Token usage from session.
		var totalInput, totalOutput, totalCacheCreate, totalCacheRead int
		msgCount := 0
		if deps.Session != nil {
			msgCount = deps.Session.MessageCount()
			for _, msg := range deps.Session.Messages {
				if msg.Usage != nil {
					totalInput += msg.Usage.InputTokens
					totalOutput += msg.Usage.OutputTokens
					totalCacheCreate += msg.Usage.CacheCreationInput
					totalCacheRead += msg.Usage.CacheReadInput
				}
			}
		} else if deps.MessageCount != nil {
			msgCount = deps.MessageCount()
		}

		model := ""
		if deps.Model != nil {
			model = deps.Model()
		}
		fmt.Fprintf(&b, "Context\n")
		fmt.Fprintf(&b, "  Model              %s\n", model)
		fmt.Fprintf(&b, "  Max tokens         %d\n", deps.Config.MaxTokens)
		fmt.Fprintf(&b, "  Messages           %d\n", msgCount)
		fmt.Fprintf(&b, "  Instruction files  %d\n", len(files))

		if totalInput > 0 || totalOutput > 0 {
			b.WriteString("\nToken usage (session)\n")
			fmt.Fprintf(&b, "  Input tokens       %s\n", formatTokens(totalInput))
			fmt.Fprintf(&b, "  Output tokens      %s\n", formatTokens(totalOutput))
			if totalCacheCreate > 0 {
				fmt.Fprintf(&b, "  Cache creation     %s\n", formatTokens(totalCacheCreate))
			}
			if totalCacheRead > 0 {
				fmt.Fprintf(&b, "  Cache read         %s\n", formatTokens(totalCacheRead))
			}
			total := totalInput + totalOutput
			fmt.Fprintf(&b, "  Total              %s\n", formatTokens(total))
		}

		if len(files) > 0 {
			b.WriteString("\nInstruction files\n")
			totalChars := 0
			for _, f := range files {
				totalChars += len(f.Content)
				fmt.Fprintf(&b, "  %s (%d chars)\n", f.Path, len(f.Content))
			}
			fmt.Fprintf(&b, "  Total: %d chars (budget: %d/%d)\n",
				totalChars, prompt.MaxFileContentBudget, prompt.MaxTotalBudget)
		}

		return b.String(), nil
	}
}

// formatTokens formats a token count in a human-readable way.
func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
