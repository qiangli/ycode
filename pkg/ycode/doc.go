// Package ycode is the public Go embedding API for ycode — the local-first
// agentic coding harness.
//
// # Quick start
//
//	a, _ := ycode.NewAgent(ycode.WithModel("claude-sonnet-4-6"))
//	_ = a.Chat(ctx, "explain the runtime", func(ev ycode.Event) { ... })
//
// The default Agent registers every built-in tool (bash, file ops, search,
// MCP, GitHub, etc.) and exposes the full /api/sessions HTTP surface via
// (*Agent).Handler. That shape is right for first-party uses (the ycode CLI
// itself, internal tooling) and wrong for third-party hosts that need a
// locked-down chat backbone.
//
// # Embedding ycode in a third-party app
//
// The experimental build tag unlocks the surface designed for that case.
// Build with -tags experimental and use:
//
//   - WithoutBuiltinTools / WithBuiltinAllowlist — opt out of the dangerous
//     defaults (bash, write_file, edit_file, Agent) and register only the
//     host's own domain tools via (*Agent).Registry().Register.
//
//   - WithPermissionResolver / WithPermissionPrompter — programmatically deny
//     elevated-permission tool invocations without prompting a human.
//
//   - HandlerWithAuth — wrap (*Agent).Handler with the host's auth
//     middleware. The middleware MUST stamp an actor.User onto r.Context()
//     via pkg/ycode/actor.WithUser; custom tools read it back with
//     actor.UserFromContext for authorization decisions.
//
//   - (*Agent).Extract — single-call structured-output extraction. JSON
//     schema in, JSON bytes out. Bypasses the agent loop, tools, and memory.
//
//   - (*Agent).Embed / EmbedBatch — vector embeddings via the same
//     env-precedence ladder used by ycode internally (OpenAI /embeddings,
//     local Ollama /api/embed, TF-IDF fallback).
//
//   - (*Agent).Provider — escape hatch for direct provider access when none
//     of the above helpers fit.
//
// # The actor seam
//
// The pkg/ycode/actor sub-package defines the User context contract. It
// ships without a build tag — third-party code that targets stable ycode
// builds can still depend on the contract even when not using the rest of
// the experimental surface. See actor for details.
package ycode
