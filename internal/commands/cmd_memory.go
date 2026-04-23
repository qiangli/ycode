package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/memory"
	"github.com/qiangli/ycode/internal/runtime/prompt"
)

// memoryHandler returns a handler that displays discovered instruction files
// and persistent memories.
func memoryHandler(deps *RuntimeDeps) HandlerFunc {
	return func(ctx context.Context, args string) (string, error) {
		workDir := deps.WorkDir
		if workDir == "" {
			return "", fmt.Errorf("working directory not set")
		}

		var b strings.Builder

		// Section 1: Discovered instruction files (CLAUDE.md ancestry).
		files := prompt.DiscoverInstructionFiles(workDir, workDir)

		fmt.Fprintf(&b, "Memory\n")
		fmt.Fprintf(&b, "  Working directory  %s\n", workDir)
		fmt.Fprintf(&b, "  Instruction files  %d\n", len(files))

		b.WriteString("\nDiscovered files\n")
		if len(files) == 0 {
			b.WriteString("  No instruction files discovered in the directory ancestry.\n")
		} else {
			for i, f := range files {
				lines := strings.Count(f.Content, "\n") + 1
				preview := firstLine(f.Content)
				fmt.Fprintf(&b, "  %d. %s\n", i+1, f.Path)
				fmt.Fprintf(&b, "     lines=%d  preview=%s\n", lines, preview)
			}
		}

		// Section 2: Persistent memories (if memory dir is configured).
		if deps.MemoryDir != "" {
			mgr, err := memory.NewManager(deps.MemoryDir)
			if err == nil {
				memories, err := mgr.All()
				if err == nil {
					b.WriteString("\nPersistent memories\n")
					if len(memories) == 0 {
						b.WriteString("  No persistent memories stored.\n")
					} else {
						fmt.Fprintf(&b, "  Count: %d\n", len(memories))
						for _, mem := range memories {
							fmt.Fprintf(&b, "  - [%s] %s: %s\n", mem.Type, mem.Name, mem.Description)
						}
					}
				}
			}
		}

		return b.String(), nil
	}
}

// firstLine returns the first non-empty line of content, or "<empty>".
func firstLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 80 {
				return line[:80] + "..."
			}
			return line
		}
	}
	return "<empty>"
}
