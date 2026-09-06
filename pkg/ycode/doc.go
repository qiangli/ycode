// Package ycode is the public Go embedding API for the YAML-native ycode
// harness.
//
// # Quick start
//
//	h, _ := ycode.Load("agent.yaml")
//	defer h.Close()
//	events, _ := h.Run(ctx, ycode.RunRequest{...})
//	for event := range events { ... }
//
// Load strictly compiles agent.yaml. Run, Resume and Fork expose the same
// append-only Event records used by every frontend, including payload digests,
// configuration identity and hash-chain ordering. Provider selection, stages,
// retry, tools, policy, memory and output routing remain YAML controls.
package ycode
