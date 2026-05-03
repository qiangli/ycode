// Example: connect to podman and print the engine version using pkg/oci.
//
// Usage:
//
//	go run .
//
// Requires a running podman service (socket).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/qiangli/ycode/pkg/oci/bindings/system"
	"github.com/qiangli/ycode/pkg/oci/bindings"
)

func main() {
	ctx := context.Background()

	// Connect to the default podman socket.
	socketPath := os.Getenv("CONTAINER_HOST")
	if socketPath == "" {
		socketPath = "unix:///run/podman/podman.sock"
	}

	fmt.Printf("Connecting to podman at %s ...\n", socketPath)

	connCtx, err := bindings.NewConnection(ctx, socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error connecting: %v\n", err)
		os.Exit(1)
	}

	info, err := system.Version(connCtx, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting version: %v\n", err)
		os.Exit(1)
	}

	if info.Server != nil {
		fmt.Printf("Podman server version: %s\n", info.Server.Version)
	}
	if info.Client != nil {
		fmt.Printf("Podman client version: %s\n", info.Client.Version)
	}
}
