// Package shell implements the interactive `ycode shell` subcommand: an
// agentic shell that is bash-compatible at the command layer (via mvdan/sh
// in-process interpreter) and LLM-mediated at the UX layer.
//
// Surface conventions:
//
//	bare words   → POSIX/bash dispatch (reserved → builtin → alias → PATH)
//	/<word>      → slash command from commands.Registry
//	@<name>      → skill from SkillResolver
//	@<path>      → skill loaded from filesystem path
//	!<text>      → one-shot agent with shell context (stub in skeleton)
//	?<text>      → cheap LLM Q&A (stub in skeleton)
//
// Sentinels only fire when they are the first non-whitespace token of a
// logical line. Inside quotes, heredocs, command-substitution, mid-line,
// pipelines, or redirections they are literal text. PATH always wins for
// bare words.
//
// Permission posture: the user is the operator, like /bin/bash itself, so
// the shell runs at permission.DangerFullAccess by default and the agent-
// mode validators (V01–V12) are not applied. Validators belong to the
// agent executor (`internal/runtime/bash` exec handler), not the shell.
//
// See plan §11–§13 in /Users/you/.claude/plans for the full design.
package shell
