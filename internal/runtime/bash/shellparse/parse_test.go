package shellparse

import (
	"testing"
)

func TestParse_SimpleCommand(t *testing.T) {
	nodes, err := Parse("ls -la")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	n := nodes[0]
	if n.Name != "ls" {
		t.Errorf("Name = %q, want %q", n.Name, "ls")
	}
	if len(n.Args) != 1 || n.Args[0] != "-la" {
		t.Errorf("Args = %v, want [-la]", n.Args)
	}
	if n.InSubshell || n.InPipeline || n.Negated {
		t.Errorf("unexpected flags: subshell=%v pipeline=%v negated=%v",
			n.InSubshell, n.InPipeline, n.Negated)
	}
}

func TestParse_QuotedOperators(t *testing.T) {
	// Quoted && should NOT be split into separate commands.
	nodes, err := Parse(`echo "hello && world"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (quoted && must not split)", len(nodes))
	}
	if nodes[0].Name != "echo" {
		t.Errorf("Name = %q, want %q", nodes[0].Name, "echo")
	}
	if len(nodes[0].Args) != 1 || nodes[0].Args[0] != "hello && world" {
		t.Errorf("Args = %v, want [hello && world]", nodes[0].Args)
	}
}

func TestParse_Pipeline(t *testing.T) {
	nodes, err := Parse("cat file | grep foo | wc -l")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("got %d nodes, want 3", len(nodes))
	}
	for i, n := range nodes {
		if !n.InPipeline {
			t.Errorf("node[%d] (%q): InPipeline = false, want true", i, n.Name)
		}
	}
	if nodes[0].Name != "cat" {
		t.Errorf("nodes[0].Name = %q, want %q", nodes[0].Name, "cat")
	}
	if nodes[1].Name != "grep" {
		t.Errorf("nodes[1].Name = %q, want %q", nodes[1].Name, "grep")
	}
	if nodes[2].Name != "wc" {
		t.Errorf("nodes[2].Name = %q, want %q", nodes[2].Name, "wc")
	}
}

func TestParse_Lists(t *testing.T) {
	nodes, err := Parse("cmd1 && cmd2 || cmd3; cmd4")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 4 {
		t.Fatalf("got %d nodes, want 4", len(nodes))
	}
	names := []string{"cmd1", "cmd2", "cmd3", "cmd4"}
	for i, want := range names {
		if nodes[i].Name != want {
			t.Errorf("nodes[%d].Name = %q, want %q", i, nodes[i].Name, want)
		}
	}
}

func TestParse_VariableAssignment(t *testing.T) {
	nodes, err := Parse("FOO=bar baz arg1")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	n := nodes[0]
	if n.Name != "baz" {
		t.Errorf("Name = %q, want %q", n.Name, "baz")
	}
	if len(n.Assigns) != 1 || n.Assigns[0] != "FOO=bar" {
		t.Errorf("Assigns = %v, want [FOO=bar]", n.Assigns)
	}
	if len(n.Args) != 1 || n.Args[0] != "arg1" {
		t.Errorf("Args = %v, want [arg1]", n.Args)
	}
}

func TestParse_BareAssignment(t *testing.T) {
	// Assignment without a command — should still produce a node.
	nodes, err := Parse("FOO=bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	n := nodes[0]
	if n.Name != "" {
		t.Errorf("Name = %q, want empty", n.Name)
	}
	if len(n.Assigns) != 1 || n.Assigns[0] != "FOO=bar" {
		t.Errorf("Assigns = %v, want [FOO=bar]", n.Assigns)
	}
}

func TestParse_CommandSubstitution(t *testing.T) {
	nodes, err := Parse("echo $(rm -rf /)")
	if err != nil {
		t.Fatal(err)
	}
	// Should produce at least 2 nodes: echo (top-level) and rm (in subshell).
	if len(nodes) < 2 {
		t.Fatalf("got %d nodes, want >= 2", len(nodes))
	}

	var echoNode, rmNode *CommandNode
	for i := range nodes {
		switch nodes[i].Name {
		case "echo":
			echoNode = &nodes[i]
		case "rm":
			rmNode = &nodes[i]
		}
	}
	if echoNode == nil {
		t.Fatal("missing echo node")
	}
	if echoNode.InSubshell {
		t.Error("echo should not be InSubshell")
	}
	if rmNode == nil {
		t.Fatal("missing rm node from command substitution")
	}
	if !rmNode.InSubshell {
		t.Error("rm inside $() should have InSubshell=true")
	}
}

func TestParse_Subshell(t *testing.T) {
	nodes, err := Parse("(cd /tmp && rm -rf dir)")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if !n.InSubshell {
			t.Errorf("node %q: InSubshell = false, want true", n.Name)
		}
	}
}

func TestParse_Redirects(t *testing.T) {
	nodes, err := Parse("echo hello > file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	if len(nodes[0].Redirects) != 1 {
		t.Fatalf("got %d redirects, want 1", len(nodes[0].Redirects))
	}
	r := nodes[0].Redirects[0]
	if r.Op != ">" {
		t.Errorf("redirect Op = %q, want %q", r.Op, ">")
	}
	if r.File != "file.txt" {
		t.Errorf("redirect File = %q, want %q", r.File, "file.txt")
	}
}

func TestParse_Redirects_Append(t *testing.T) {
	nodes, err := Parse("echo hello >> log.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes[0].Redirects) != 1 {
		t.Fatalf("got %d redirects, want 1", len(nodes[0].Redirects))
	}
	r := nodes[0].Redirects[0]
	if r.Op != ">>" {
		t.Errorf("redirect Op = %q, want %q", r.Op, ">>")
	}
}

func TestParse_Redirects_Stderr(t *testing.T) {
	nodes, err := Parse("cmd 2>/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes[0].Redirects) != 1 {
		t.Fatalf("got %d redirects, want 1", len(nodes[0].Redirects))
	}
	r := nodes[0].Redirects[0]
	if r.Op != "2>" {
		t.Errorf("redirect Op = %q, want %q", r.Op, "2>")
	}
	if r.File != "/dev/null" {
		t.Errorf("redirect File = %q, want %q", r.File, "/dev/null")
	}
}

func TestParse_HereDoc(t *testing.T) {
	nodes, err := Parse("cat <<EOF\nhello\nEOF")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	if len(nodes[0].Redirects) != 1 {
		t.Fatalf("got %d redirects, want 1", len(nodes[0].Redirects))
	}
	r := nodes[0].Redirects[0]
	if r.Op != "<<" {
		t.Errorf("redirect Op = %q, want %q", r.Op, "<<")
	}
}

func TestParse_PathQualifiedCommand(t *testing.T) {
	nodes, err := Parse("/usr/bin/git status")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	if nodes[0].Name != "git" {
		t.Errorf("Name = %q, want %q (path should be stripped)", nodes[0].Name, "git")
	}
}

func TestParse_Negated(t *testing.T) {
	nodes, err := Parse("! grep -q pattern file")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	if !nodes[0].Negated {
		t.Error("Negated = false, want true")
	}
}

func TestParse_IfClause(t *testing.T) {
	nodes, err := Parse("if true; then rm -rf /tmp/dir; fi")
	if err != nil {
		t.Fatal(err)
	}
	// Should find "true" from condition and "rm" from then body.
	var foundRM bool
	for _, n := range nodes {
		if n.Name == "rm" {
			foundRM = true
		}
	}
	if !foundRM {
		t.Error("did not find rm node inside if body")
	}
}

func TestParse_ForLoop(t *testing.T) {
	nodes, err := Parse("for f in *.log; do rm \"$f\"; done")
	if err != nil {
		t.Fatal(err)
	}
	var foundRM bool
	for _, n := range nodes {
		if n.Name == "rm" {
			foundRM = true
		}
	}
	if !foundRM {
		t.Error("did not find rm node inside for loop body")
	}
}

func TestParse_WhileLoop(t *testing.T) {
	nodes, err := Parse("while true; do sleep 1; done")
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, n := range nodes {
		if n.Name != "" {
			names[n.Name] = true
		}
	}
	if !names["true"] {
		t.Error("did not find 'true' in condition")
	}
	if !names["sleep"] {
		t.Error("did not find 'sleep' in body")
	}
}

func TestParse_CaseStatement(t *testing.T) {
	nodes, err := Parse(`case "$1" in start) echo starting;; stop) echo stopping;; esac`)
	if err != nil {
		t.Fatal(err)
	}
	echoCount := 0
	for _, n := range nodes {
		if n.Name == "echo" {
			echoCount++
		}
	}
	if echoCount != 2 {
		t.Errorf("got %d echo nodes, want 2", echoCount)
	}
}

func TestParse_ProcessSubstitution(t *testing.T) {
	nodes, err := Parse("diff <(cmd1) <(cmd2)")
	if err != nil {
		t.Fatal(err)
	}
	// Should find diff (top-level), cmd1 and cmd2 (in subshell context).
	names := make(map[string]bool)
	subshellNames := make(map[string]bool)
	for _, n := range nodes {
		names[n.Name] = true
		if n.InSubshell {
			subshellNames[n.Name] = true
		}
	}
	if !names["diff"] {
		t.Error("missing diff node")
	}
	if !subshellNames["cmd1"] {
		t.Error("cmd1 should be InSubshell")
	}
	if !subshellNames["cmd2"] {
		t.Error("cmd2 should be InSubshell")
	}
}

func TestParse_BacktickSubstitution(t *testing.T) {
	nodes, err := Parse("echo `whoami`")
	if err != nil {
		t.Fatal(err)
	}
	var whoamiNode *CommandNode
	for i := range nodes {
		if nodes[i].Name == "whoami" {
			whoamiNode = &nodes[i]
		}
	}
	if whoamiNode == nil {
		t.Fatal("missing whoami node from backtick substitution")
	}
	if !whoamiNode.InSubshell {
		t.Error("whoami inside backticks should have InSubshell=true")
	}
}

func TestParse_DeclClause(t *testing.T) {
	nodes, err := Parse("export FOO=bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	if nodes[0].Name != "export" {
		t.Errorf("Name = %q, want %q", nodes[0].Name, "export")
	}
}

func TestParse_FunctionDecl(t *testing.T) {
	nodes, err := Parse("myfunc() { rm -rf /tmp/junk; }")
	if err != nil {
		t.Fatal(err)
	}
	var foundRM bool
	for _, n := range nodes {
		if n.Name == "rm" {
			foundRM = true
		}
	}
	if !foundRM {
		t.Error("did not find rm inside function body")
	}
}

func TestParse_MalformedInput(t *testing.T) {
	// Malformed bash should return an error, not panic.
	_, err := Parse("if then fi else")
	if err == nil {
		t.Error("expected parse error for malformed input, got nil")
	}
}

func TestParse_EmptyCommand(t *testing.T) {
	nodes, err := Parse("")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("got %d nodes for empty command, want 0", len(nodes))
	}
}

func TestParse_SingleQuotedOperators(t *testing.T) {
	nodes, err := Parse("echo '&&' '||' ';'")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	if nodes[0].Name != "echo" {
		t.Errorf("Name = %q, want %q", nodes[0].Name, "echo")
	}
}

func TestParse_NestedCommandSubstitution(t *testing.T) {
	nodes, err := Parse("echo $(cat $(find . -name '*.txt'))")
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, n := range nodes {
		names[n.Name] = true
	}
	if !names["echo"] {
		t.Error("missing echo")
	}
	if !names["cat"] {
		t.Error("missing cat from outer $()")
	}
	if !names["find"] {
		t.Error("missing find from inner $()")
	}
}

func TestParse_CompoundInPipeline(t *testing.T) {
	nodes, err := Parse("echo foo | (cat && wc -l)")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		switch n.Name {
		case "echo":
			if !n.InPipeline {
				t.Error("echo should be InPipeline")
			}
			if n.InSubshell {
				t.Error("echo should not be InSubshell")
			}
		case "cat", "wc":
			if !n.InSubshell {
				t.Errorf("%s should be InSubshell (inside parentheses)", n.Name)
			}
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add("ls -la")
	f.Add("echo 'hello && world'")
	f.Add("cat file | grep foo && rm -rf /")
	f.Add("FOO=bar baz")
	f.Add("echo $(rm -rf /)")
	f.Add("if true; then echo ok; fi")
	f.Add("")
	f.Add(";;;")
	f.Add("((((")
	f.Add(`echo "unterminated`)

	f.Fuzz(func(t *testing.T, cmd string) {
		// Must never panic regardless of input.
		Parse(cmd) //nolint:errcheck
	})
}
