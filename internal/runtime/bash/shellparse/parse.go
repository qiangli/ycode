// Package shellparse provides AST-based bash command parsing for safety analysis.
//
// It uses mvdan.cc/sh to parse shell commands into a structured representation
// that safety.go can classify accurately — handling quoting, subshells,
// pipelines, command substitutions, and control flow correctly.
package shellparse

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// CommandNode is a flattened representation of a shell command segment,
// extracted from the AST for safety classification.
type CommandNode struct {
	Name       string     // base command name (path-stripped, e.g., "rm", "git")
	Args       []string   // literal arguments (dequoted); does not include the command name
	Redirects  []Redirect // output/input redirects on this command
	Assigns    []string   // VAR=val prefix assignments
	InSubshell bool       // true if inside $(...), backticks, or (...)
	InPipeline bool       // true if this is part of a pipeline
	Negated    bool       // preceded by !
}

// Redirect represents a single I/O redirection.
type Redirect struct {
	Op   string // ">", ">>", "2>", "&>", "<", "<<", "<<<", etc.
	File string // target filename (may be empty for heredocs)
}

// Parse extracts all CommandNodes from a shell command string.
// It walks the full AST including pipelines, lists, subshells,
// command substitutions, for/while/if bodies, and function declarations.
//
// Returns an error only if parsing fails entirely; callers should fall
// back to string-based splitting in that case.
func Parse(command string) ([]CommandNode, error) {
	parser := syntax.NewParser(
		syntax.Variant(syntax.LangBash),
		syntax.KeepComments(false),
	)

	f, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, err
	}

	var nodes []CommandNode
	w := &walker{nodes: &nodes}
	w.walkFile(f)
	return nodes, nil
}

// walker traverses the syntax tree, collecting CommandNodes.
type walker struct {
	nodes      *[]CommandNode
	inSubshell bool
	inPipeline bool
}

func (w *walker) walkFile(f *syntax.File) {
	for _, stmt := range f.Stmts {
		w.walkStmt(stmt)
	}
}

func (w *walker) walkStmt(stmt *syntax.Stmt) {
	if stmt.Cmd == nil {
		return
	}

	negated := stmt.Negated
	redirs := extractRedirects(stmt.Redirs)

	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		w.walkCallExpr(cmd, negated, redirs)
	case *syntax.BinaryCmd:
		w.walkBinaryCmd(cmd)
	case *syntax.Subshell:
		w.walkSubshell(cmd)
	case *syntax.Block:
		w.walkBlock(cmd)
	case *syntax.IfClause:
		w.walkIfClause(cmd)
	case *syntax.WhileClause:
		w.walkWhileClause(cmd)
	case *syntax.ForClause:
		w.walkForClause(cmd)
	case *syntax.CaseClause:
		w.walkCaseClause(cmd)
	case *syntax.FuncDecl:
		w.walkStmt(cmd.Body)
	case *syntax.DeclClause:
		w.walkDeclClause(cmd, negated, redirs)
	case *syntax.ArithmCmd:
		// arithmetic: (( expr )) — no command to classify
	case *syntax.TestClause:
		// [[ expr ]] — no command to classify
	case *syntax.TimeClause:
		if cmd.Stmt != nil {
			w.walkStmt(cmd.Stmt)
		}
	case *syntax.CoprocClause:
		if cmd.Stmt != nil {
			w.walkStmt(cmd.Stmt)
		}
	case *syntax.LetClause:
		// let expr — no command to classify
	}
}

func (w *walker) walkCallExpr(call *syntax.CallExpr, negated bool, stmtRedirs []Redirect) {
	node := CommandNode{
		InSubshell: w.inSubshell,
		InPipeline: w.inPipeline,
		Negated:    negated,
		Redirects:  stmtRedirs,
	}

	// Extract variable assignments.
	for _, assign := range call.Assigns {
		if assign.Name != nil {
			name := assign.Name.Value
			val := wordToString(assign.Value)
			node.Assigns = append(node.Assigns, name+"="+val)
		}
	}

	// Extract command name and arguments from Args.
	if len(call.Args) > 0 {
		node.Name = extractCommandName(call.Args[0])
		for _, arg := range call.Args[1:] {
			node.Args = append(node.Args, wordToString(arg))
		}
	}

	// Also walk into any command substitutions in arguments to detect
	// hidden commands (e.g., echo $(rm -rf /)).
	for _, arg := range call.Args {
		w.walkWordParts(arg)
	}
	for _, assign := range call.Assigns {
		if assign.Value != nil {
			w.walkWordParts(assign.Value)
		}
	}

	*w.nodes = append(*w.nodes, node)
}

func (w *walker) walkBinaryCmd(cmd *syntax.BinaryCmd) {
	if cmd.Op == syntax.Pipe || cmd.Op == syntax.PipeAll {
		saved := w.inPipeline
		w.inPipeline = true
		w.walkStmt(cmd.X)
		w.walkStmt(cmd.Y)
		w.inPipeline = saved
	} else {
		// && or ||: each side is an independent command.
		w.walkStmt(cmd.X)
		w.walkStmt(cmd.Y)
	}
}

func (w *walker) walkSubshell(cmd *syntax.Subshell) {
	saved := w.inSubshell
	w.inSubshell = true
	for _, stmt := range cmd.Stmts {
		w.walkStmt(stmt)
	}
	w.inSubshell = saved
}

func (w *walker) walkBlock(cmd *syntax.Block) {
	for _, stmt := range cmd.Stmts {
		w.walkStmt(stmt)
	}
}

func (w *walker) walkIfClause(cmd *syntax.IfClause) {
	for _, stmt := range cmd.Cond {
		w.walkStmt(stmt)
	}
	for _, stmt := range cmd.Then {
		w.walkStmt(stmt)
	}
	if cmd.Else != nil {
		w.walkIfClause(cmd.Else)
	}
}

func (w *walker) walkWhileClause(cmd *syntax.WhileClause) {
	for _, stmt := range cmd.Cond {
		w.walkStmt(stmt)
	}
	for _, stmt := range cmd.Do {
		w.walkStmt(stmt)
	}
}

func (w *walker) walkForClause(cmd *syntax.ForClause) {
	for _, stmt := range cmd.Do {
		w.walkStmt(stmt)
	}
}

func (w *walker) walkCaseClause(cmd *syntax.CaseClause) {
	for _, item := range cmd.Items {
		for _, stmt := range item.Stmts {
			w.walkStmt(stmt)
		}
	}
}

func (w *walker) walkDeclClause(cmd *syntax.DeclClause, negated bool, stmtRedirs []Redirect) {
	variant := ""
	if cmd.Variant != nil {
		variant = cmd.Variant.Value
	}
	node := CommandNode{
		Name:       variant,
		InSubshell: w.inSubshell,
		InPipeline: w.inPipeline,
		Negated:    negated,
		Redirects:  stmtRedirs,
	}
	for _, assign := range cmd.Args {
		if assign.Name != nil {
			node.Args = append(node.Args, assign.Name.Value)
		}
	}
	*w.nodes = append(*w.nodes, node)
}

// walkWordParts recursively descends into word parts looking for
// command substitutions ($(...) and backticks) and process substitutions.
func (w *walker) walkWordParts(word *syntax.Word) {
	if word == nil {
		return
	}
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.CmdSubst:
			saved := w.inSubshell
			w.inSubshell = true
			for _, stmt := range p.Stmts {
				w.walkStmt(stmt)
			}
			w.inSubshell = saved
		case *syntax.ProcSubst:
			saved := w.inSubshell
			w.inSubshell = true
			for _, stmt := range p.Stmts {
				w.walkStmt(stmt)
			}
			w.inSubshell = saved
		case *syntax.DblQuoted:
			// Recurse into double-quoted parts for nested substitutions.
			for _, inner := range p.Parts {
				switch ip := inner.(type) {
				case *syntax.CmdSubst:
					saved := w.inSubshell
					w.inSubshell = true
					for _, stmt := range ip.Stmts {
						w.walkStmt(stmt)
					}
					w.inSubshell = saved
				case *syntax.ProcSubst:
					saved := w.inSubshell
					w.inSubshell = true
					for _, stmt := range ip.Stmts {
						w.walkStmt(stmt)
					}
					w.inSubshell = saved
				}
			}
		}
	}
}

// extractCommandName gets the base command name from the first word of a CallExpr.
// It strips directory paths (e.g., /usr/bin/git -> git) and handles
// simple literals only. Complex expansions return an empty string.
func extractCommandName(word *syntax.Word) string {
	lit := wordLit(word)
	if lit == "" {
		return ""
	}
	return filepath.Base(lit)
}

// wordLit returns the literal string value of a word if it consists
// entirely of Lit parts. Returns "" if the word contains expansions.
func wordLit(word *syntax.Word) string {
	if word == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			// Only extract if all inner parts are literals.
			for _, inner := range p.Parts {
				switch ip := inner.(type) {
				case *syntax.Lit:
					b.WriteString(ip.Value)
				default:
					return ""
				}
			}
		default:
			return ""
		}
	}
	return b.String()
}

// wordToString converts a Word to its string representation.
// For words with expansions, it returns a best-effort literal representation.
func wordToString(word *syntax.Word) string {
	if word == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				switch ip := inner.(type) {
				case *syntax.Lit:
					b.WriteString(ip.Value)
				default:
					// For expansions inside quotes, include a placeholder.
					b.WriteString("$?")
				}
			}
		case *syntax.ParamExp:
			if p.Param != nil {
				b.WriteString("$" + p.Param.Value)
			} else {
				b.WriteString("$?")
			}
		case *syntax.CmdSubst:
			b.WriteString("$(…)")
		case *syntax.ArithmExp:
			b.WriteString("$((…))")
		case *syntax.ProcSubst:
			b.WriteString("<(…)")
		default:
			b.WriteString("?")
		}
	}
	return b.String()
}

// extractRedirects converts syntax.Redirect nodes to our Redirect type.
func extractRedirects(redirs []*syntax.Redirect) []Redirect {
	if len(redirs) == 0 {
		return nil
	}
	result := make([]Redirect, 0, len(redirs))
	for _, r := range redirs {
		rd := Redirect{
			Op: r.Op.String(),
		}
		if r.N != nil {
			// Prefix the fd number: e.g., "2" + ">" = "2>"
			rd.Op = r.N.Value + rd.Op
		}
		if r.Word != nil {
			rd.File = wordToString(r.Word)
		}
		result = append(result, rd)
	}
	return result
}
