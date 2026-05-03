package bash

import (
	"testing"
)

func TestValidateCommandSubstitution(t *testing.T) {
	tests := []struct {
		cmd string
		ok  bool
	}{
		{"ls -la", true},
		{"echo hello", true},
		{"echo $(whoami)", false},
		{"echo `whoami`", false},
		{"cat file.txt", true},
	}
	for _, tt := range tests {
		r := ValidateCommandSubstitution(tt.cmd)
		if r.OK != tt.ok {
			t.Errorf("ValidateCommandSubstitution(%q) = %v, want %v (%s)", tt.cmd, r.OK, tt.ok, r.Reason)
		}
	}
}

func TestValidateProcessSubstitution(t *testing.T) {
	tests := []struct {
		cmd string
		ok  bool
	}{
		{"diff file1 file2", true},
		{"diff <(sort file1) <(sort file2)", false},
		{"tee >(grep error) >(grep warn)", false},
	}
	for _, tt := range tests {
		r := ValidateProcessSubstitution(tt.cmd)
		if r.OK != tt.ok {
			t.Errorf("ValidateProcessSubstitution(%q) = %v, want %v", tt.cmd, r.OK, tt.ok)
		}
	}
}

func TestValidateZshDangerous(t *testing.T) {
	tests := []struct {
		cmd string
		ok  bool
	}{
		{"ls", true},
		{"zmodload zsh/net/tcp", false},
		{"sysopen -w /etc/passwd", false},
		{"ztcp localhost 8080", false},
		{"zpty -r output", false},
		{"git status", true},
	}
	for _, tt := range tests {
		r := ValidateZshDangerous(tt.cmd)
		if r.OK != tt.ok {
			t.Errorf("ValidateZshDangerous(%q) = %v, want %v", tt.cmd, r.OK, tt.ok)
		}
	}
}

func TestValidateIFSInjection(t *testing.T) {
	r := ValidateIFSInjection("IFS=/ cat /etc/passwd")
	if r.OK {
		t.Error("IFS injection should be blocked")
	}
	r = ValidateIFSInjection("echo hello")
	if !r.OK {
		t.Error("normal command should pass")
	}
}

func TestValidateBlockedSleep(t *testing.T) {
	tests := []struct {
		cmd string
		ok  bool
	}{
		{"sleep 1", true},
		{"sleep 2", false},
		{"sleep 60", false},
		{"echo hello && sleep 5", false},
		{"echo sleep 10", true}, // "sleep" is not a command here
	}
	for _, tt := range tests {
		r := ValidateBlockedSleep(tt.cmd)
		if r.OK != tt.ok {
			t.Errorf("ValidateBlockedSleep(%q) = %v, want %v (%s)", tt.cmd, r.OK, tt.ok, r.Reason)
		}
	}
}

func TestValidateBlockedDevices(t *testing.T) {
	tests := []struct {
		cmd string
		ok  bool
	}{
		{"cat /dev/null", true},
		{"cat /dev/zero", false},
		{"cat /dev/urandom", false},
		{"cat /dev/stdin", false},
		{"cat /proc/123/fd/0", false},
		{"cat /tmp/file.txt", true},
	}
	for _, tt := range tests {
		r := ValidateBlockedDevices(tt.cmd)
		if r.OK != tt.ok {
			t.Errorf("ValidateBlockedDevices(%q) = %v, want %v", tt.cmd, r.OK, tt.ok)
		}
	}
}

func TestValidateUnicodeControl(t *testing.T) {
	r := ValidateUnicodeControl("echo hello")
	if !r.OK {
		t.Error("normal text should pass")
	}

	// Zero-width space U+200B.
	r = ValidateUnicodeControl("echo\u200Bhello")
	if r.OK {
		t.Error("zero-width space should be detected")
	}

	// Bidi override U+202E.
	r = ValidateUnicodeControl("echo\u202Ehello")
	if r.OK {
		t.Error("bidi override should be detected")
	}
}

func TestValidateBraceExpansion(t *testing.T) {
	tests := []struct {
		cmd string
		ok  bool
	}{
		{"echo hello", true},
		{"echo {a..z}", false},
		{"touch file{1..100}.txt", false},
		{"echo {a,b,c}", true}, // comma expansion is ok
	}
	for _, tt := range tests {
		r := ValidateBraceExpansion(tt.cmd)
		if r.OK != tt.ok {
			t.Errorf("ValidateBraceExpansion(%q) = %v, want %v", tt.cmd, r.OK, tt.ok)
		}
	}
}

func TestValidateSedInPlace(t *testing.T) {
	tests := []struct {
		cmd string
		ok  bool
	}{
		{"sed 's/foo/bar/' file.txt", true},         // no -i, just print
		{"sed -i 's/foo/bar/' file.txt", false},     // in-place edit
		{"sed -i.bak 's/foo/bar/' file.txt", false}, // in-place with backup
		{"grep foo", true},
	}
	for _, tt := range tests {
		r := ValidateSedInPlace(tt.cmd)
		if r.OK != tt.ok {
			t.Errorf("ValidateSedInPlace(%q) = %v, want %v", tt.cmd, r.OK, tt.ok)
		}
	}
}

func TestValidateEvalExec(t *testing.T) {
	tests := []struct {
		cmd string
		ok  bool
	}{
		{"echo hello", true},
		{"eval echo hello", false},
		{"echo eval", true}, // eval as argument, not command
	}
	for _, tt := range tests {
		r := ValidateEvalExec(tt.cmd)
		if r.OK != tt.ok {
			t.Errorf("ValidateEvalExec(%q) = %v, want %v", tt.cmd, r.OK, tt.ok)
		}
	}
}

func TestValidateHeredocExpansion(t *testing.T) {
	r := ValidateHeredocExpansion("cat << EOF\nhello\nEOF")
	if !r.OK {
		t.Error("heredoc without variable expansion should pass")
	}

	r = ValidateHeredocExpansion("cat << EOF\n${SECRET}\nEOF")
	if r.OK {
		t.Error("heredoc with variable expansion should be blocked")
	}

	r = ValidateHeredocExpansion("cat <<'EOF'\n${SECRET}\nEOF")
	if !r.OK {
		t.Error("quoted heredoc delimiter should pass (expansion disabled)")
	}
}

func TestRunAllValidators(t *testing.T) {
	// Safe command.
	r := RunAllValidators("ls -la /tmp")
	if !r.OK {
		t.Errorf("safe command should pass: %s", r.Reason)
	}

	// Unsafe command.
	r = RunAllValidators("eval $(cat /etc/passwd)")
	if r.OK {
		t.Error("dangerous command should fail")
	}
}

func TestValidateBacktickNesting(t *testing.T) {
	r := ValidateBacktickNesting("echo `date`")
	if !r.OK {
		t.Error("single level backtick should pass")
	}

	r = ValidateBacktickNesting("echo `echo `echo `echo ``")
	if r.OK {
		t.Error("deeply nested backticks should be blocked")
	}
}
