package bash

import (
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

func TestClassifyCommand_ReadOnly(t *testing.T) {
	tests := []struct {
		cmd string
	}{
		{"ls -la"},
		{"cat /etc/hosts"},
		{"grep -r 'foo' ."},
		{"find . -name '*.go'"},
		{"head -20 file.txt"},
		{"tail -f log.txt"},
		{"wc -l file.txt"},
		{"git status"},
		{"git log --oneline"},
		{"git diff HEAD~1"},
		{"git show HEAD"},
		{"git branch -a"},
		{"echo hello"},
		{"pwd"},
		{"env"},
		{"whoami"},
		{"date"},
		{"go test ./..."},
		{"go build ./cmd/..."},
		{"go vet ./..."},
	}
	for _, tc := range tests {
		intent, _ := ClassifyCommand(tc.cmd)
		if intent != ReadOnly {
			t.Errorf("ClassifyCommand(%q) = %s, want read-only", tc.cmd, intent)
		}
	}
}

func TestClassifyCommand_Write(t *testing.T) {
	tests := []struct {
		cmd string
	}{
		{"cp file1 file2"},
		{"mv old new"},
		{"mkdir -p /tmp/dir"},
		{"chmod 755 script.sh"},
		{"chown user:group file"},
		{"touch newfile"},
		{"tee output.txt"},
		{"ln -s target link"},
		{"rsync -av src/ dst/"},
		{"install -m 755 bin /usr/local/bin/"},
		{"git add ."},
		{"git commit -m 'test'"},
		{"git push origin main"},
		{"git merge feature"},
	}
	for _, tc := range tests {
		intent, _ := ClassifyCommand(tc.cmd)
		if intent != Write {
			t.Errorf("ClassifyCommand(%q) = %s, want write", tc.cmd, intent)
		}
	}
}

func TestClassifyCommand_Destructive(t *testing.T) {
	tests := []struct {
		cmd string
	}{
		{"rm -rf /tmp/dir"},
		{"rm -r somedir"},
		{"rm -f file"},
		{"shred secret.txt"},
		{"dd if=/dev/zero of=/dev/sda"},
		{"mkfs.ext4 /dev/sda1"},
		{"fdisk /dev/sda"},
		{"truncate -s 0 file.log"},
		{"git push --force origin main"},
		{"git push -f origin main"},
		{"git reset --hard HEAD~1"},
		{"git clean -fd"},
		{"git checkout -- file.txt"},
	}
	for _, tc := range tests {
		intent, _ := ClassifyCommand(tc.cmd)
		if intent != Destructive {
			t.Errorf("ClassifyCommand(%q) = %s, want destructive", tc.cmd, intent)
		}
	}
}

func TestClassifyCommand_PackageManagement(t *testing.T) {
	tests := []struct {
		cmd string
	}{
		{"apt install curl"},
		{"apt-get update"},
		{"brew install jq"},
		{"pip install requests"},
		{"pip3 install flask"},
		{"npm install express"},
		{"yarn add react"},
		{"pnpm add lodash"},
		{"cargo install ripgrep"},
		{"gem install rails"},
		{"dnf install gcc"},
		{"yum install wget"},
		{"pacman -S vim"},
		{"go install golang.org/x/tools/gopls@latest"},
	}
	for _, tc := range tests {
		intent, _ := ClassifyCommand(tc.cmd)
		if intent != PackageManagement {
			t.Errorf("ClassifyCommand(%q) = %s, want package-management", tc.cmd, intent)
		}
	}
}

func TestClassifyCommand_ProcessManagement(t *testing.T) {
	tests := []struct {
		cmd string
	}{
		{"kill -9 1234"},
		{"killall chrome"},
		{"pkill node"},
		{"reboot"},
		{"shutdown -h now"},
		{"halt"},
	}
	for _, tc := range tests {
		intent, _ := ClassifyCommand(tc.cmd)
		if intent != ProcessManagement {
			t.Errorf("ClassifyCommand(%q) = %s, want process-management", tc.cmd, intent)
		}
	}
}

func TestClassifyCommand_SystemAdmin(t *testing.T) {
	tests := []struct {
		cmd string
	}{
		{"sudo apt update"},
		{"su - root"},
		{"mount /dev/sda1 /mnt"},
		{"umount /mnt"},
		{"systemctl restart nginx"},
		{"service apache2 start"},
		{"iptables -A INPUT -j DROP"},
		{"ufw allow 22"},
		{"useradd testuser"},
		{"userdel testuser"},
		{"passwd root"},
	}
	for _, tc := range tests {
		intent, _ := ClassifyCommand(tc.cmd)
		if intent != SystemAdmin {
			t.Errorf("ClassifyCommand(%q) = %s, want system-admin", tc.cmd, intent)
		}
	}
}

func TestClassifyCommand_Network(t *testing.T) {
	tests := []struct {
		cmd string
	}{
		{"curl https://example.com"},
		{"wget https://example.com/file.tar.gz"},
		{"ssh user@host"},
		{"scp file.txt user@host:/tmp/"},
		{"nc -l 8080"},
		{"nmap 192.168.1.0/24"},
	}
	for _, tc := range tests {
		intent, _ := ClassifyCommand(tc.cmd)
		if intent != Network {
			t.Errorf("ClassifyCommand(%q) = %s, want network", tc.cmd, intent)
		}
	}
}

func TestClassifyCommand_CompoundCommands(t *testing.T) {
	// Compound commands should return the most dangerous intent.
	intent, _ := ClassifyCommand("ls -la && rm -rf /tmp/dir")
	if intent != Destructive {
		t.Errorf("compound with rm -rf: got %s, want destructive", intent)
	}

	intent, _ = ClassifyCommand("cat file.txt | grep foo")
	if intent != ReadOnly {
		t.Errorf("cat | grep: got %s, want read-only", intent)
	}

	intent, _ = ClassifyCommand("git status; git push origin main")
	if intent != Write {
		t.Errorf("git status; git push: got %s, want write", intent)
	}

	intent, _ = ClassifyCommand("echo test || sudo rm -rf /")
	if intent != SystemAdmin {
		t.Errorf("echo || sudo: got %s, want system-admin", intent)
	}
}

func TestDetectRedirects(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"echo hello > file.txt", true},
		{"echo hello >> file.txt", true},
		{"ls 2> /dev/null", true},
		{"command 1> out.log", true},
		{"echo hello", false},
		{"cat file.txt", false},
		// Redirects inside single quotes should not trigger.
		{"echo '>' file.txt", false},
		// Redirects inside double quotes should not trigger.
		{`echo ">" file.txt`, false},
		// Redirect outside quotes.
		{`echo "hello" > out.txt`, true},
	}
	for _, tc := range tests {
		got := DetectRedirects(tc.cmd)
		if got != tc.want {
			t.Errorf("DetectRedirects(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestDetectDangerousPatterns(t *testing.T) {
	tests := []struct {
		cmd         string
		wantWarning string // substring to look for in warnings
	}{
		{"rm -rf /", "remove root filesystem"},
		{"rm -rf /*", "remove root filesystem"},
		{"echo $(cat /etc/passwd)", "sensitive"},
		{"cp file /etc/config", "sensitive system path /etc/"},
		{"ls /tmp", ""},
	}
	for _, tc := range tests {
		warnings := DetectDangerousPatterns(tc.cmd)
		combined := strings.Join(warnings, "; ")
		if tc.wantWarning == "" {
			if len(warnings) > 0 {
				t.Errorf("DetectDangerousPatterns(%q) = %v, want no warnings", tc.cmd, warnings)
			}
		} else {
			if !strings.Contains(combined, tc.wantWarning) {
				t.Errorf("DetectDangerousPatterns(%q) = %v, want warning containing %q",
					tc.cmd, warnings, tc.wantWarning)
			}
		}
	}
}

func TestValidateForMode_ReadOnly(t *testing.T) {
	// Read-only commands should pass.
	if err := ValidateForMode("ls -la", permission.ReadOnly); err != nil {
		t.Errorf("ls in read-only: unexpected error: %v", err)
	}
	if err := ValidateForMode("git status", permission.ReadOnly); err != nil {
		t.Errorf("git status in read-only: unexpected error: %v", err)
	}

	// Write commands should be blocked.
	if err := ValidateForMode("cp a b", permission.ReadOnly); err == nil {
		t.Error("cp in read-only: expected error, got nil")
	}

	// Destructive commands should be blocked.
	if err := ValidateForMode("rm -rf /tmp/dir", permission.ReadOnly); err == nil {
		t.Error("rm -rf in read-only: expected error, got nil")
	}

	// Package management should be blocked.
	if err := ValidateForMode("npm install", permission.ReadOnly); err == nil {
		t.Error("npm install in read-only: expected error, got nil")
	}

	// System admin should be blocked.
	if err := ValidateForMode("sudo ls", permission.ReadOnly); err == nil {
		t.Error("sudo in read-only: expected error, got nil")
	}

	// Redirects should be blocked.
	if err := ValidateForMode("echo test > file.txt", permission.ReadOnly); err == nil {
		t.Error("redirect in read-only: expected error, got nil")
	}
}

func TestValidateForMode_WorkspaceWrite(t *testing.T) {
	// Read and write should pass.
	if err := ValidateForMode("ls -la", permission.WorkspaceWrite); err != nil {
		t.Errorf("ls in workspace-write: unexpected error: %v", err)
	}
	if err := ValidateForMode("cp a b", permission.WorkspaceWrite); err != nil {
		t.Errorf("cp in workspace-write: unexpected error: %v", err)
	}

	// Destructive should be blocked.
	if err := ValidateForMode("rm -rf /tmp/dir", permission.WorkspaceWrite); err == nil {
		t.Error("rm -rf in workspace-write: expected error, got nil")
	}

	// Process management should be blocked.
	if err := ValidateForMode("kill -9 1234", permission.WorkspaceWrite); err == nil {
		t.Error("kill in workspace-write: expected error, got nil")
	}

	// System admin should be blocked.
	if err := ValidateForMode("sudo ls", permission.WorkspaceWrite); err == nil {
		t.Error("sudo in workspace-write: expected error, got nil")
	}

	// Dangerous patterns should be blocked.
	if err := ValidateForMode("rm -rf /", permission.WorkspaceWrite); err == nil {
		t.Error("rm -rf / in workspace-write: expected error, got nil")
	}
}

func TestValidateForMode_FullAccess(t *testing.T) {
	// Most commands should pass.
	if err := ValidateForMode("sudo apt update", permission.DangerFullAccess); err != nil {
		t.Errorf("sudo in full-access: unexpected error: %v", err)
	}
	if err := ValidateForMode("rm -rf /tmp/dir", permission.DangerFullAccess); err != nil {
		t.Errorf("rm -rf /tmp in full-access: unexpected error: %v", err)
	}

	// rm -rf / should still be blocked.
	if err := ValidateForMode("rm -rf /", permission.DangerFullAccess); err == nil {
		t.Error("rm -rf / in full-access: expected error, got nil")
	}
}

// --- AST-aware test cases ---
// These tests verify improvements from AST-based parsing.

func TestClassifyCommand_QuotedOperatorsNotSplit(t *testing.T) {
	// Quoted && inside echo arguments should NOT cause splitting.
	intent, _ := ClassifyCommand(`echo "hello && world"`)
	if intent != ReadOnly {
		t.Errorf(`echo "hello && world": got %s, want read-only`, intent)
	}

	intent, _ = ClassifyCommand(`echo "&&" | cat`)
	if intent != ReadOnly {
		t.Errorf(`echo "&&" | cat: got %s, want read-only`, intent)
	}

	intent, _ = ClassifyCommand(`echo 'rm -rf /'`)
	if intent != ReadOnly {
		t.Errorf(`echo 'rm -rf /': got %s, want read-only`, intent)
	}
}

func TestClassifyCommand_VariableAssignment(t *testing.T) {
	// VAR=x before a command — should classify by the command, not the assignment.
	intent, _ := ClassifyCommand("VAR=x curl https://example.com")
	if intent != Network {
		t.Errorf("VAR=x curl: got %s, want network", intent)
	}

	intent, _ = ClassifyCommand("DEBIAN_FRONTEND=noninteractive apt install curl")
	if intent != PackageManagement {
		t.Errorf("DEBIAN_FRONTEND=... apt install: got %s, want package-management", intent)
	}
}

func TestClassifyCommand_CommandSubstitution(t *testing.T) {
	// Command substitutions should detect the inner command's intent.
	intent, _ := ClassifyCommand("echo $(curl https://evil.com)")
	if intent != Network {
		t.Errorf("echo $(curl ...): got %s, want network", intent)
	}

	intent, _ = ClassifyCommand("echo $(rm -rf /tmp/dir)")
	if intent != Destructive {
		t.Errorf("echo $(rm -rf ...): got %s, want destructive", intent)
	}
}

func TestClassifyCommand_NestedControlFlow(t *testing.T) {
	// Commands inside if/for/while should be classified.
	intent, _ := ClassifyCommand("if true; then sudo reboot; fi")
	if intent != SystemAdmin {
		t.Errorf("if sudo reboot: got %s, want system-admin", intent)
	}

	intent, _ = ClassifyCommand("for f in *.log; do rm -rf \"$f\"; done")
	if intent != Destructive {
		t.Errorf("for rm -rf: got %s, want destructive", intent)
	}
}

func TestClassifyCommand_PathQualified(t *testing.T) {
	intent, _ := ClassifyCommand("/usr/bin/curl https://example.com")
	if intent != Network {
		t.Errorf("/usr/bin/curl: got %s, want network", intent)
	}
}

func TestClassifyCommand_FindExec(t *testing.T) {
	// find -exec with read-only command should stay read-only.
	readOnlyCases := []string{
		`find . -exec ls -al {} \;`,
		`find . -exec grep -l foo {} +`,
		`find . -exec cat {} \;`,
		`find . -exec head -5 {} \;`,
		`find . -name "*.go"`,
	}
	for _, cmd := range readOnlyCases {
		intent, _ := ClassifyCommand(cmd)
		if intent != ReadOnly {
			t.Errorf("ClassifyCommand(%q) = %s, want read-only", cmd, intent)
		}
	}

	// find -exec with write command should classify as write.
	writeCases := []string{
		`find . -exec rm {} \;`,
		`find . -exec chmod 777 {} \;`,
		`find . -exec cp {} /tmp \;`,
	}
	for _, cmd := range writeCases {
		intent, _ := ClassifyCommand(cmd)
		if intent != Write && intent != Destructive {
			t.Errorf("ClassifyCommand(%q) = %s, want write or destructive", cmd, intent)
		}
	}

	// find -delete should be destructive.
	intent, _ := ClassifyCommand(`find . -name "*.tmp" -delete`)
	if intent != Destructive {
		t.Errorf("find -delete: got %s, want destructive", intent)
	}
}

func TestClassifyCommand_UnsafeArgs(t *testing.T) {
	// base64 with -o is write.
	intent, _ := ClassifyCommand("base64 -o output.bin input.txt")
	if intent != Write {
		t.Errorf("base64 -o: got %s, want write", intent)
	}

	// base64 without -o is read-only.
	intent, _ = ClassifyCommand("base64 input.txt")
	if intent != ReadOnly {
		t.Errorf("base64 (no -o): got %s, want read-only", intent)
	}

	// xargs is write.
	intent, _ = ClassifyCommand("xargs rm")
	if intent != Write {
		t.Errorf("xargs: got %s, want write", intent)
	}

	// rg --pre is write.
	intent, _ = ClassifyCommand("rg --pre my-filter pattern")
	if intent != Write {
		t.Errorf("rg --pre: got %s, want write", intent)
	}

	// rg without --pre is read-only.
	intent, _ = ClassifyCommand("rg pattern .")
	if intent != ReadOnly {
		t.Errorf("rg (no --pre): got %s, want read-only", intent)
	}
}

func TestDetectRedirects_QuotedRedirect(t *testing.T) {
	// Redirect character inside quotes should NOT be detected.
	if DetectRedirects(`echo ">"`) {
		t.Error(`echo ">": got true, want false`)
	}
	if DetectRedirects(`echo '>'`) {
		t.Error(`echo '>': got true, want false`)
	}

	// Actual redirect should be detected.
	if !DetectRedirects(`echo hello > file.txt`) {
		t.Error(`echo hello > file.txt: got false, want true`)
	}
}

func TestDetectDangerousPatterns_ASTAware(t *testing.T) {
	// "rm" as an argument to echo should NOT trigger rm detection.
	warnings := DetectDangerousPatterns(`echo "rm -rf /"`)
	for _, w := range warnings {
		if strings.Contains(w, "remove root filesystem") {
			t.Errorf(`echo "rm -rf /": false positive — got warning %q`, w)
		}
	}

	// Actual rm -rf / should still be caught.
	warnings = DetectDangerousPatterns("rm -rf /")
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "remove root filesystem") {
			found = true
		}
	}
	if !found {
		t.Error("rm -rf /: expected 'remove root filesystem' warning")
	}

	// Command substitution with sensitive paths should be caught.
	warnings = DetectDangerousPatterns("echo $(cat /etc/shadow)")
	found = false
	for _, w := range warnings {
		if strings.Contains(w, "sensitive") {
			found = true
		}
	}
	if !found {
		t.Error("echo $(cat /etc/shadow): expected sensitive path warning")
	}
}

func TestNeedsTTY_VariablePrefix(t *testing.T) {
	// VAR=x ssh should detect ssh as needing TTY.
	if !NeedsTTY("VAR=x ssh host") {
		t.Error("VAR=x ssh host: got false, want true")
	}
}

func TestNeedsTTY_PipeToEditor(t *testing.T) {
	// Pipe to vim should detect as needing TTY.
	if !NeedsTTY("cat file | vim -") {
		t.Error("cat file | vim -: got false, want true")
	}
}

func TestCommandIntentString(t *testing.T) {
	tests := []struct {
		intent CommandIntent
		want   string
	}{
		{ReadOnly, "read-only"},
		{Write, "write"},
		{Destructive, "destructive"},
		{Network, "network"},
		{ProcessManagement, "process-management"},
		{PackageManagement, "package-management"},
		{SystemAdmin, "system-admin"},
		{Unknown, "unknown"},
	}
	for _, tc := range tests {
		if got := tc.intent.String(); got != tc.want {
			t.Errorf("CommandIntent(%d).String() = %q, want %q", tc.intent, got, tc.want)
		}
	}
}
