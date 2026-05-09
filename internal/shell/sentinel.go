package shell

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// IntentKind identifies the dispatch route for a submitted line.
type IntentKind int

const (
	// IntentBash routes the line to the in-process bash interpreter.
	IntentBash IntentKind = iota
	// IntentSlash routes to commands.Registry via Name (the part after `/`).
	IntentSlash
	// IntentSkill routes to SkillResolver.Resolve(Name).
	IntentSkill
	// IntentSkillPath routes to SkillResolver.ResolvePath(Path).
	IntentSkillPath
	// IntentAgentShot is the `!<text>` one-shot agent invocation. Args holds the text.
	IntentAgentShot
	// IntentAgentQA is the `?<text>` cheap-LLM-Q&A invocation. Args holds the text.
	IntentAgentQA
	// IntentEmpty is whitespace-only or empty input.
	IntentEmpty
)

// String returns a debug-friendly name for the kind.
func (k IntentKind) String() string {
	switch k {
	case IntentBash:
		return "bash"
	case IntentSlash:
		return "slash"
	case IntentSkill:
		return "skill"
	case IntentSkillPath:
		return "skill-path"
	case IntentAgentShot:
		return "agent-shot"
	case IntentAgentQA:
		return "agent-qa"
	case IntentEmpty:
		return "empty"
	}
	return "unknown"
}

// Intent is the parsed classification of a submitted line.
type Intent struct {
	Kind IntentKind
	Name string // /slash name (no leading slash) or @skill identifier
	Path string // filesystem path for IntentSkillPath
	Args string // text after the sentinel token (trimmed of leading whitespace)
	Raw  string // original input verbatim

	// Upstream is non-empty when the sentinel sits at the tail of a pipe,
	// e.g. `ls -la | @summarize` — the dispatcher runs Upstream as bash,
	// captures stdout, and feeds it to the agent/skill as additional input.
	// Only `@` sentinels participate; `/` slash commands never have an
	// Upstream. Skeleton supports only the simple Y-is-sentinel case;
	// see plan §13b.
	Upstream string

	// Downstream is non-empty when the sentinel sits at the head of a
	// pipe, e.g. `@summarize | tee out` — the dispatcher captures the
	// agent's text output and pipes it into Downstream's bash via
	// stdin. Mutually exclusive with Upstream. Only `@` sentinels
	// participate.
	Downstream string
}

// ErrSentinelInPipeline reports that a sentinel appeared at the start of a
// pipeline, list, redirection, background job, or other multi-command form.
// Disallowed in v1 — see plan §12c.
var ErrSentinelInPipeline = errors.New("sentinel commands cannot appear in pipelines or redirections")

var (
	// slashIdent: `/` followed by [A-Za-z0-9_-]+ and nothing else (no second slash).
	slashIdent = regexp.MustCompile(`^/[A-Za-z0-9_-]+$`)
	// skillIdent: `@` followed by [A-Za-z0-9_-]+ (no `/` or `.`).
	skillIdent = regexp.MustCompile(`^@[A-Za-z0-9_-]+$`)
)

// Classify routes a submitted line to one of the IntentKind variants.
//
// Rules (plan §12):
//   - Sentinels (/, @, !, ?) only fire when they are the first
//     non-whitespace character of the line.
//   - / and @ go through the AST: a single Stmt whose Cmd is a CallExpr
//     with a single-Lit first word. Quoting, escapes, expansions, or
//     multi-command structures (pipe, list, redirect, background) revert
//     to bash — except when they would have been a sentinel, in which
//     case ErrSentinelInPipeline is returned.
//   - ! and ? are checked at the raw-text level, because bash uses `!`
//     for command negation. In shell mode the `!` sentinel takes
//     precedence over bash negation by design (see plan §12c notes).
func Classify(raw string) (Intent, error) {
	_, end := StartSpan(context.Background(), "ycode.shell.classify")
	intent, err := classify(raw)
	observeIntent(intent.Kind.String())
	if err != nil {
		end(err, "kind", intent.Kind.String())
	} else {
		end(nil, "kind", intent.Kind.String())
	}
	return intent, err
}

func classify(raw string) (Intent, error) {
	trimmed := strings.TrimLeft(raw, " \t")
	if trimmed == "" {
		return Intent{Kind: IntentEmpty, Raw: raw}, nil
	}

	switch trimmed[0] {
	case '?':
		return Intent{Kind: IntentAgentQA, Args: strings.TrimSpace(trimmed[1:]), Raw: raw}, nil
	case '!':
		return Intent{Kind: IntentAgentShot, Args: strings.TrimSpace(trimmed[1:]), Raw: raw}, nil
	case '/', '@':
		return classifyAST(raw, trimmed[0])
	default:
		// Bash line that may end in a pipe-to-sentinel: `ls | @summarize`.
		// Cheap pre-filter avoids a parse on every keystroke when there
		// is obviously no `@` in the input.
		if strings.Contains(raw, "@") {
			if intent, ok := classifyTrailingPipeToSentinel(raw); ok {
				return intent, nil
			}
		}
		return Intent{Kind: IntentBash, Raw: raw}, nil
	}
}

// classifyTrailingPipeToSentinel parses `raw` and reports a pipe-to-sentinel
// intent when the program is a single Stmt of the form `<bash> | @<id>` or
// `<bash> | @<path>`. Returns false on any other shape, so the caller falls
// through to plain bash.
func classifyTrailingPipeToSentinel(raw string) (Intent, bool) {
	prog, err := syntax.NewParser().Parse(strings.NewReader(raw), "")
	if err != nil {
		return Intent{}, false
	}
	if len(prog.Stmts) != 1 {
		return Intent{}, false
	}
	return classifyPipeToSentinel(prog.Stmts[0], raw)
}

// classifyAST handles `/` and `@` — both require AST inspection to tell a
// sentinel apart from a bash path / quoted literal, and to detect pipeline
// misuse or pipe-to-sentinel forms. `leadingChar` is the trimmed leading
// character (already known to be `/` or `@`); `raw` is the untouched input.
func classifyAST(raw string, leadingChar byte) (Intent, error) {
	prog, err := syntax.NewParser().Parse(strings.NewReader(raw), "")
	if err != nil {
		// Not valid bash either. Hand to the interpreter so the user sees
		// a real bash parse error rather than a sentinel-routing error.
		return Intent{Kind: IntentBash, Raw: raw}, nil
	}
	if len(prog.Stmts) == 0 {
		return Intent{Kind: IntentBash, Raw: raw}, nil
	}
	if len(prog.Stmts) > 1 {
		// Multiple statements separated by `;` or newline — sentinel form
		// requires exactly one command.
		return Intent{Raw: raw}, ErrSentinelInPipeline
	}

	stmt := prog.Stmts[0]

	// Pipe-to-sentinel (`<bash> | @<id>`) and sentinel-source pipe
	// (`@<id> | <bash>`). Only `@` participates; `/` is always
	// sentinel-error in pipelines.
	if leadingChar == '@' {
		if intent, ok := classifyPipeToSentinel(stmt, raw); ok {
			return intent, nil
		}
		if intent, ok := classifySourcePipeSentinel(stmt, raw); ok {
			return intent, nil
		}
	}

	if isMultiCmd(stmt) {
		return Intent{Raw: raw}, ErrSentinelInPipeline
	}
	if stmt.Negated {
		// `! /foo` — bash negation, not a sentinel. (The agent `!` form
		// was handled at the raw-text level before we got here.)
		return Intent{Kind: IntentBash, Raw: raw}, nil
	}

	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return Intent{Kind: IntentBash, Raw: raw}, nil
	}

	firstWord := call.Args[0]
	if len(firstWord.Parts) != 1 {
		// Quoting, escapes, or expansions in the first word → bash.
		return Intent{Kind: IntentBash, Raw: raw}, nil
	}
	lit, ok := firstWord.Parts[0].(*syntax.Lit)
	if !ok {
		return Intent{Kind: IntentBash, Raw: raw}, nil
	}

	val := lit.Value
	endOff := int(firstWord.End().Offset())
	args := ""
	if endOff < len(raw) {
		args = strings.TrimLeft(raw[endOff:], " \t")
	}

	switch leadingChar {
	case '/':
		if slashIdent.MatchString(val) {
			return Intent{Kind: IntentSlash, Name: val[1:], Args: args, Raw: raw}, nil
		}
		// /usr/bin/ls etc. — filesystem path, normal bash.
		return Intent{Kind: IntentBash, Raw: raw}, nil

	case '@':
		body := val[1:]
		if body == "" {
			return Intent{Kind: IntentBash, Raw: raw}, nil
		}
		if strings.ContainsAny(body, "/.") {
			return Intent{Kind: IntentSkillPath, Path: body, Args: args, Raw: raw}, nil
		}
		if skillIdent.MatchString(val) {
			return Intent{Kind: IntentSkill, Name: body, Args: args, Raw: raw}, nil
		}
		return Intent{Kind: IntentBash, Raw: raw}, nil
	}
	return Intent{Kind: IntentBash, Raw: raw}, nil
}

// classifyPipeToSentinel detects the `<bash> | @<id-or-path>` form. The
// stmt's top-level command is a BinaryCmd with Op=Pipe whose right side
// (Y) is a sentinel CallExpr. Returns the matching IntentSkill /
// IntentSkillPath with Upstream set to the printed left side.
func classifyPipeToSentinel(stmt *syntax.Stmt, raw string) (Intent, bool) {
	if stmt.Negated || stmt.Background || stmt.Coprocess || len(stmt.Redirs) > 0 {
		return Intent{}, false
	}
	bc, ok := stmt.Cmd.(*syntax.BinaryCmd)
	if !ok || (bc.Op != syntax.Pipe && bc.Op != syntax.PipeAll) {
		return Intent{}, false
	}

	// Inspect Y — must be a single, single-Lit-first-word CallExpr that
	// matches a `@` sentinel form.
	yStmt := bc.Y
	if yStmt.Negated || yStmt.Background || yStmt.Coprocess || len(yStmt.Redirs) > 0 {
		return Intent{}, false
	}
	yCall, ok := yStmt.Cmd.(*syntax.CallExpr)
	if !ok || len(yCall.Args) == 0 {
		return Intent{}, false
	}
	yFirst := yCall.Args[0]
	if len(yFirst.Parts) != 1 {
		return Intent{}, false
	}
	yLit, ok := yFirst.Parts[0].(*syntax.Lit)
	if !ok || len(yLit.Value) < 2 || yLit.Value[0] != '@' {
		return Intent{}, false
	}

	body := yLit.Value[1:]

	// Args after the sentinel token, taken from the raw text starting at
	// yFirst's end offset — preserves the user's literal spacing.
	yEndOff := int(yFirst.End().Offset())
	args := ""
	if yEndOff < len(raw) {
		args = strings.TrimLeft(raw[yEndOff:], " \t")
	}

	upstream := printNode(bc.X)

	switch {
	case strings.ContainsAny(body, "/."):
		return Intent{Kind: IntentSkillPath, Path: body, Args: args, Raw: raw, Upstream: upstream}, true
	case skillIdent.MatchString(yLit.Value):
		return Intent{Kind: IntentSkill, Name: body, Args: args, Raw: raw, Upstream: upstream}, true
	default:
		return Intent{}, false
	}
}

// classifySourcePipeSentinel detects the `@<id-or-path> | <bash>` form —
// the sentinel is the source of the pipe and its text output streams
// into Downstream's bash. Mirrors classifyPipeToSentinel but inspects
// the X (left) side of the BinaryCmd and serializes the Y (right) side
// as Downstream.
func classifySourcePipeSentinel(stmt *syntax.Stmt, raw string) (Intent, bool) {
	if stmt.Negated || stmt.Background || stmt.Coprocess || len(stmt.Redirs) > 0 {
		return Intent{}, false
	}
	bc, ok := stmt.Cmd.(*syntax.BinaryCmd)
	if !ok || (bc.Op != syntax.Pipe && bc.Op != syntax.PipeAll) {
		return Intent{}, false
	}

	xStmt := bc.X
	if xStmt.Negated || xStmt.Background || xStmt.Coprocess || len(xStmt.Redirs) > 0 {
		return Intent{}, false
	}
	xCall, ok := xStmt.Cmd.(*syntax.CallExpr)
	if !ok || len(xCall.Args) == 0 {
		return Intent{}, false
	}
	xFirst := xCall.Args[0]
	if len(xFirst.Parts) != 1 {
		return Intent{}, false
	}
	xLit, ok := xFirst.Parts[0].(*syntax.Lit)
	if !ok || len(xLit.Value) < 2 || xLit.Value[0] != '@' {
		return Intent{}, false
	}

	body := xLit.Value[1:]

	// Args between the sentinel token and the `|` operator. End of the
	// X subtree marks the boundary.
	xEndOff := int(xCall.End().Offset())
	xFirstEnd := int(xFirst.End().Offset())
	args := ""
	if xFirstEnd < xEndOff {
		args = strings.TrimSpace(raw[xFirstEnd:xEndOff])
	}

	downstream := printNode(bc.Y)

	switch {
	case strings.ContainsAny(body, "/."):
		return Intent{Kind: IntentSkillPath, Path: body, Args: args, Raw: raw, Downstream: downstream}, true
	case skillIdent.MatchString(xLit.Value):
		return Intent{Kind: IntentSkill, Name: body, Args: args, Raw: raw, Downstream: downstream}, true
	default:
		return Intent{}, false
	}
}

// printNode serializes an AST subtree back to source.
func printNode(n syntax.Node) string {
	var b bytes.Buffer
	_ = syntax.NewPrinter().Print(&b, n)
	return strings.TrimSpace(b.String())
}

// isMultiCmd reports whether a Stmt has structure beyond a single foreground
// CallExpr — a pipeline, list (&& || ;), redirection, background, or
// coprocess. The sentinel form requires a bare single-command Stmt.
func isMultiCmd(s *syntax.Stmt) bool {
	if s.Background || s.Coprocess || len(s.Redirs) > 0 {
		return true
	}
	switch s.Cmd.(type) {
	case *syntax.BinaryCmd:
		return true
	}
	return false
}
