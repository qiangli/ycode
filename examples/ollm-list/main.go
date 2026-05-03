// Example: list models from a local Ollama server using pkg/ollm.
//
// Usage:
//
//	go run .
//	OLLAMA_HOST=http://localhost:11434 go run .
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/qiangli/ycode/pkg/ollm"
)

func main() {
	ctx := context.Background()
	baseURL := ollm.DefaultURL()

	fmt.Printf("Connecting to Ollama at %s ...\n", baseURL)

	if !ollm.Detect(ctx, baseURL) {
		fmt.Println("Ollama server not reachable. Start Ollama and try again.")
		os.Exit(1)
	}

	client, err := ollm.NewClient(baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	models, err := client.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing models: %v\n", err)
		os.Exit(1)
	}

	if len(models) == 0 {
		fmt.Println("No models found.")
		return
	}

	fmt.Printf("%-40s %s\n", "NAME", "SIZE")
	for _, m := range models {
		fmt.Printf("%-40s %d MB\n", m.Name, m.Size/1024/1024)
	}
}
