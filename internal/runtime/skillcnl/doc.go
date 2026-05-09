//go:build experimental

// Package skillcnl implements the multilingual Controlled Natural
// Language layer for ycode skills. See docs/skill-cnl.md for the
// four-layer architecture and the role of each component.
//
// Phase 0 ships the deterministic core: the dhnt encoder, the closed
// multilingual glossary, the Layer 2 typed AST, the Layer 1 / Layer 1.5
// linearisers, and the Layer 1.5 parser. The LLM normaliser (Layer 0
// to Layer 2) and the Wasm Component Model leaves (Layer 3) are
// separable follow-ups.
//
// Validity is defined by transpilability: a skill expression is valid
// iff it transpiles cleanly to Layer 1.5 dhnt. The dhnt encoder is
// the validator.
package skillcnl
