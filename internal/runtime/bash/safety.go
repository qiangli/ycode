package bash

import (
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/bash/shellparse"
	"github.com/qiangli/ycode/internal/runtime/permission"
)

// CommandIntent classifies the safety level of a bash command.
type CommandIntent int

const (
	// ReadOnly commands only read data (ls, cat, grep, etc.).
	ReadOnly CommandIntent = iota
	// Write commands modify files or directories.
	Write
	// Destructive commands can cause irreversible data loss.
	Destructive
	// Network commands communicate over the network.
	Network
	// ProcessManagement commands manage system processes.
	ProcessManagement
	// PackageManagement commands install or remove packages.
	PackageManagement
	// SystemAdmin commands require elevated privileges.
	SystemAdmin
	// Unknown commands cannot be classified.
	Unknown
)

// String returns a human-readable name for the intent.
func (c CommandIntent) String() string {
	switch c {
	case ReadOnly:
		return "read-only"
	case Write:
		return "write"
	case Destructive:
		return "destructive"
	case Network:
		return "network"
	case ProcessManagement:
		return "process-management"
	case PackageManagement:
		return "package-management"
	case SystemAdmin:
		return "system-admin"
	case Unknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// intentPriority returns the danger level for ordering (higher = more dangerous).
func intentPriority(c CommandIntent) int {
	switch c {
	case ReadOnly:
		return 0
	case Write:
		return 1
	case Network:
		return 2
	case PackageManagement:
		return 3
	case ProcessManagement:
		return 4
	case Destructive:
		return 5
	case SystemAdmin:
		return 6
	case Unknown:
		return 7
	default:
		return 7
	}
}

// ClassifyCommand analyzes a command string and returns its most dangerous
// intent along with a list of reasons for the classification.
func ClassifyCommand(command string) (CommandIntent, []string) {
	// Try AST-based classification first for accuracy.
	nodes, err := shellparse.Parse(command)
	if err == nil && len(nodes) > 0 {
		return classifyNodes(nodes)
	}

	// Fallback to string-based splitting on parse error.
	return classifyStringBased(command)
}

// classifyNodes classifies parsed CommandNodes.
func classifyNodes(nodes []shellparse.CommandNode) (CommandIntent, []string) {
	maxIntent := ReadOnly
	var allReasons []string

	for _, node := range nodes {
		intent, reasons := classifyNode(node)
		if intentPriority(intent) > intentPriority(maxIntent) {
			maxIntent = intent
		}
		allReasons = append(allReasons, reasons...)
	}

	return maxIntent, allReasons
}

// classifyNode classifies a single parsed command node.
func classifyNode(node shellparse.CommandNode) (CommandIntent, []string) {
	base := node.Name
	if base == "" {
		return ReadOnly, nil
	}

	// System admin commands.
	if isSystemAdmin(base) {
		return SystemAdmin, []string{fmt.Sprintf("%q is a system administration command", base)}
	}

	// Process management.
	if isProcessManagement(base) {
		return ProcessManagement, []string{fmt.Sprintf("%q is a process management command", base)}
	}

	// Package managers — build a fields-like slice for compatibility.
	fields := append([]string{base}, node.Args...)
	if isPackageManager(base, fields) {
		return PackageManagement, []string{fmt.Sprintf("%q is a package management command", base)}
	}

	// Destructive commands.
	rest := strings.Join(node.Args, " ")
	if intent, reason := checkDestructive(base, fields, rest); intent == Destructive {
		return Destructive, []string{reason}
	}

	// Write commands.
	if isWriteCommand(base) {
		return Write, []string{fmt.Sprintf("%q modifies the filesystem", base)}
	}

	// Argument-level safety validation for otherwise-safe commands.
	// Some read-only commands become write/dangerous with specific flags.
	if intent, reason := checkUnsafeArgs(base, node.Args); intent != ReadOnly {
		return intent, []string{reason}
	}

	// Git operations.
	if base == "git" && len(node.Args) > 0 {
		return classifyGit(node.Args)
	}

	// Network commands.
	if isNetworkCommand(base) {
		return Network, []string{fmt.Sprintf("%q is a network command", base)}
	}

	return ReadOnly, nil
}

// classifyStringBased is the fallback path using simple string splitting.
func classifyStringBased(command string) (CommandIntent, []string) {
	segments := splitCommandSegments(command)

	maxIntent := ReadOnly
	var allReasons []string

	for _, seg := range segments {
		intent, reasons := classifySegment(seg)
		if intentPriority(intent) > intentPriority(maxIntent) {
			maxIntent = intent
		}
		allReasons = append(allReasons, reasons...)
	}

	return maxIntent, allReasons
}

// splitCommandSegments splits a command on &&, ||, ;, and | boundaries.
// This is a simple split that does not account for quoting, which is
// acceptable for safety analysis (over-classification is safe).
func splitCommandSegments(command string) []string {
	// Replace multi-char separators first, then split on single char.
	cmd := command
	cmd = strings.ReplaceAll(cmd, "&&", "\x00")
	cmd = strings.ReplaceAll(cmd, "||", "\x00")
	cmd = strings.ReplaceAll(cmd, ";", "\x00")
	cmd = strings.ReplaceAll(cmd, "|", "\x00")

	parts := strings.Split(cmd, "\x00")
	var segments []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			segments = append(segments, p)
		}
	}
	if len(segments) == 0 {
		segments = []string{strings.TrimSpace(command)}
	}
	return segments
}

// classifySegment classifies a single command segment.
func classifySegment(segment string) (CommandIntent, []string) {
	fields := strings.Fields(segment)
	if len(fields) == 0 {
		return ReadOnly, nil
	}
	base := baseCommand(fields[0])
	rest := strings.Join(fields[1:], " ")

	// System admin commands.
	if isSystemAdmin(base) {
		return SystemAdmin, []string{fmt.Sprintf("%q is a system administration command", base)}
	}

	// Process management.
	if isProcessManagement(base) {
		return ProcessManagement, []string{fmt.Sprintf("%q is a process management command", base)}
	}

	// Package managers.
	if isPackageManager(base, fields) {
		return PackageManagement, []string{fmt.Sprintf("%q is a package management command", base)}
	}

	// Destructive commands.
	if intent, reason := checkDestructive(base, fields, rest); intent == Destructive {
		return Destructive, []string{reason}
	}

	// Write commands.
	if isWriteCommand(base) {
		return Write, []string{fmt.Sprintf("%q modifies the filesystem", base)}
	}

	// Argument-level safety validation.
	if intent, reason := checkUnsafeArgs(base, fields[1:]); intent != ReadOnly {
		return intent, []string{reason}
	}

	// Git operations.
	if base == "git" && len(fields) > 1 {
		return classifyGit(fields[1:])
	}

	// Network commands.
	if isNetworkCommand(base) {
		return Network, []string{fmt.Sprintf("%q is a network command", base)}
	}

	return ReadOnly, nil
}

// baseCommand extracts the command name from a possible path.
func baseCommand(s string) string {
	// Handle env vars like VAR=value cmd
	if strings.Contains(s, "=") && !strings.HasPrefix(s, "-") {
		return ""
	}
	idx := strings.LastIndex(s, "/")
	if idx >= 0 {
		return s[idx+1:]
	}
	return s
}

var systemAdminCmds = map[string]bool{
	"sudo": true, "su": true, "mount": true, "umount": true,
	"systemctl": true, "service": true, "iptables": true, "ufw": true,
	"useradd": true, "userdel": true, "passwd": true,
}

func isSystemAdmin(base string) bool {
	return systemAdminCmds[base]
}

var processManagementCmds = map[string]bool{
	"kill": true, "killall": true, "pkill": true,
	"reboot": true, "shutdown": true, "halt": true, "init": true,
}

func isProcessManagement(base string) bool {
	return processManagementCmds[base]
}

var packageManagerCmds = map[string]bool{
	"apt": true, "apt-get": true, "brew": true,
	"pip": true, "pip3": true, "npm": true, "yarn": true, "pnpm": true,
	"cargo": true, "gem": true, "dnf": true, "yum": true, "pacman": true,
}

func isPackageManager(base string, fields []string) bool {
	if packageManagerCmds[base] {
		return true
	}
	// "go install" is a package management command.
	if base == "go" && len(fields) > 1 && fields[1] == "install" {
		return true
	}
	return false
}

var destructiveCmds = map[string]bool{
	"shred": true, "dd": true, "mkfs": true, "fdisk": true,
}

func checkDestructive(base string, fields []string, rest string) (CommandIntent, string) {
	if destructiveCmds[base] {
		return Destructive, fmt.Sprintf("%q is a destructive command", base)
	}
	// Handle variants like mkfs.ext4, mkfs.xfs, etc.
	for cmd := range destructiveCmds {
		if strings.HasPrefix(base, cmd+".") || strings.HasPrefix(base, cmd+"-") {
			return Destructive, fmt.Sprintf("%q is a destructive command (variant of %s)", base, cmd)
		}
	}
	if base == "rm" {
		if hasRecursiveOrForce(fields[1:]) {
			return Destructive, "rm with recursive/force flags is destructive"
		}
	}
	if base == "truncate" && len(fields) > 1 {
		return Destructive, "truncate can cause data loss"
	}
	return ReadOnly, ""
}

// hasRecursiveOrForce checks for -r, -f, -rf, -fr, or combined flags containing r or f.
func hasRecursiveOrForce(args []string) bool {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue
		}
		flags := strings.TrimLeft(a, "-")
		if strings.ContainsAny(flags, "rf") {
			return true
		}
	}
	return false
}

// checkUnsafeArgs detects when otherwise-safe commands become dangerous due to
// specific arguments. Inspired by Codex CLI's argument-level safety analysis.
func checkUnsafeArgs(base string, args []string) (CommandIntent, string) {
	switch base {
	case "find":
		// find with -exec, -execdir, -ok, -okdir, -delete can modify/execute.
		for _, arg := range args {
			switch arg {
			case "-exec", "-execdir", "-ok", "-okdir":
				return Write, "find with -exec/-execdir can execute arbitrary commands"
			case "-delete":
				return Destructive, "find with -delete removes matched files"
			}
		}
	case "base64":
		// base64 with -o/--output writes to a file.
		for _, arg := range args {
			if arg == "-o" || arg == "--output" || strings.HasPrefix(arg, "--output=") {
				return Write, "base64 with -o/--output writes to file"
			}
		}
	case "xargs":
		// xargs executes commands with piped input.
		return Write, "xargs executes commands from input"
	case "rg", "ripgrep":
		// rg with --pre runs a preprocessor command.
		for _, arg := range args {
			if arg == "--pre" || strings.HasPrefix(arg, "--pre=") {
				return Write, "rg with --pre executes a preprocessor command"
			}
		}
	case "git":
		// git with global options can hijack config/execution.
		for _, arg := range args {
			if arg == "-c" || arg == "-C" || arg == "--git-dir" || arg == "--config" {
				return Write, "git with global config options can execute arbitrary code"
			}
			// Stop checking at first non-flag (subcommand).
			if !strings.HasPrefix(arg, "-") {
				break
			}
		}
	}
	return ReadOnly, ""
}

var writeCmds = map[string]bool{
	"cp": true, "mv": true, "mkdir": true, "chmod": true, "chown": true,
	"touch": true, "tee": true, "install": true, "ln": true, "rsync": true,
}

func isWriteCommand(base string) bool {
	return writeCmds[base]
}

// classifyGit classifies git subcommands.
func classifyGit(args []string) (CommandIntent, []string) {
	if len(args) == 0 {
		return ReadOnly, nil
	}
	sub := args[0]

	// Read-only git commands.
	readOnlyGit := map[string]bool{
		"status": true, "log": true, "diff": true, "show": true,
		"branch": true, "tag": true, "remote": true, "stash": true,
		"describe": true, "shortlog": true, "blame": true, "ls-files": true,
		"ls-tree": true, "rev-parse": true, "config": true,
	}
	if readOnlyGit[sub] {
		return ReadOnly, nil
	}

	// Destructive git operations.
	if sub == "push" {
		for _, a := range args[1:] {
			if a == "--force" || a == "-f" || a == "--force-with-lease" {
				return Destructive, []string{"git push with force flag is destructive"}
			}
		}
		return Write, []string{"git push modifies the remote repository"}
	}
	if sub == "reset" {
		for _, a := range args[1:] {
			if a == "--hard" {
				return Destructive, []string{"git reset --hard discards uncommitted changes"}
			}
		}
		return Write, []string{"git reset modifies the working tree"}
	}
	if sub == "clean" {
		return Destructive, []string{"git clean removes untracked files"}
	}
	if sub == "checkout" {
		for i, a := range args[1:] {
			if a == "--" && i < len(args)-2 {
				return Destructive, []string{"git checkout -- discards uncommitted changes to files"}
			}
		}
		return Write, []string{"git checkout modifies the working tree"}
	}

	// Other git write commands.
	writeGit := map[string]bool{
		"add": true, "commit": true, "merge": true, "rebase": true,
		"cherry-pick": true, "pull": true, "fetch": true, "clone": true,
		"init": true, "rm": true, "mv": true,
	}
	if writeGit[sub] {
		return Write, []string{fmt.Sprintf("git %s modifies the repository", sub)}
	}

	return ReadOnly, nil
}

var networkCmds = map[string]bool{
	"curl": true, "wget": true, "ssh": true, "scp": true,
	"sftp": true, "nc": true, "netcat": true, "nmap": true,
}

func isNetworkCommand(base string) bool {
	return networkCmds[base]
}

// DetectRedirects returns true if the command contains output redirects
// (>, >>, >&, 2>) outside of quoted strings.
func DetectRedirects(command string) bool {
	// Try AST-based detection first.
	nodes, err := shellparse.Parse(command)
	if err == nil {
		for _, node := range nodes {
			if len(node.Redirects) > 0 {
				return true
			}
		}
		return false
	}

	// Fallback to rune-walking on parse error.
	return detectRedirectsStringBased(command)
}

// detectRedirectsStringBased is the fallback redirect detection using rune scanning.
func detectRedirectsStringBased(command string) bool {
	inSingle := false
	inDouble := false
	runes := []rune(command)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		// Track quote state.
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		// Skip escaped characters inside double quotes.
		if ch == '\\' && inDouble && i+1 < len(runes) {
			i++
			continue
		}

		if inSingle || inDouble {
			continue
		}

		// Check for redirect operators.
		if ch == '>' {
			return true
		}
		// 2> or 1> style redirects.
		if (ch == '1' || ch == '2') && i+1 < len(runes) && runes[i+1] == '>' {
			return true
		}
	}
	return false
}

// sensitivePaths are system paths that should not be modified.
var sensitivePaths = []string{"/etc/", "/usr/", "/boot/", "/sys/", "/proc/"}

// DetectDangerousPatterns scans a command for dangerous patterns and returns
// a list of warning messages.
func DetectDangerousPatterns(command string) []string {
	// Try AST-based detection first.
	nodes, err := shellparse.Parse(command)
	if err == nil && len(nodes) > 0 {
		return detectDangerousPatternsAST(nodes)
	}

	// Fallback to string-based detection.
	return detectDangerousPatternsStringBased(command)
}

// detectDangerousPatternsAST uses parsed command nodes for context-aware detection.
func detectDangerousPatternsAST(nodes []shellparse.CommandNode) []string {
	var warnings []string

	for _, node := range nodes {
		// rm -rf / detection — only when "rm" is the actual command.
		if node.Name == "rm" {
			for _, arg := range node.Args {
				if strings.HasPrefix(arg, "-") {
					continue
				}
				if arg == "/" || arg == "/*" || arg == "/." || arg == "/.." {
					warnings = append(warnings, "command attempts to remove root filesystem")
				}
			}
		}

		// Commands in subshells targeting sensitive paths.
		if node.InSubshell {
			allArgs := strings.Join(node.Args, " ")
			for _, sp := range sensitivePaths {
				if strings.Contains(allArgs, sp) {
					warnings = append(warnings, fmt.Sprintf("command substitution targets sensitive path %s", sp))
					break
				}
			}
		}

		// Any command (not just arguments to echo) targeting sensitive paths.
		// Only flag for commands that actually write.
		if isWriteCommand(node.Name) || node.Name == "rm" || destructiveCmds[node.Name] {
			allArgs := strings.Join(node.Args, " ")
			for _, sp := range sensitivePaths {
				if strings.Contains(allArgs, sp) {
					warnings = append(warnings, fmt.Sprintf("command targets sensitive system path %s", sp))
					break
				}
			}
		}
	}

	return warnings
}

// detectDangerousPatternsStringBased is the fallback using raw string matching.
func detectDangerousPatternsStringBased(command string) []string {
	var warnings []string

	// rm -rf / or rm -rf /*
	lower := strings.ToLower(command)
	if strings.Contains(lower, "rm") {
		segments := splitCommandSegments(command)
		for _, seg := range segments {
			fields := strings.Fields(seg)
			if len(fields) < 2 {
				continue
			}
			if baseCommand(fields[0]) != "rm" {
				continue
			}
			for _, arg := range fields[1:] {
				if strings.HasPrefix(arg, "-") {
					continue
				}
				if arg == "/" || arg == "/*" || arg == "/." || arg == "/.." {
					warnings = append(warnings, "command attempts to remove root filesystem")
				}
			}
		}
	}

	// Command substitution writing to sensitive paths.
	hasSubstitution := strings.Contains(command, "$(") || strings.Contains(command, "`")
	if hasSubstitution {
		for _, sp := range sensitivePaths {
			if strings.Contains(command, sp) {
				warnings = append(warnings, fmt.Sprintf("command substitution targets sensitive path %s", sp))
				break
			}
		}
	}

	// Any command targeting sensitive paths.
	for _, sp := range sensitivePaths {
		if strings.Contains(command, sp) {
			warnings = append(warnings, fmt.Sprintf("command targets sensitive system path %s", sp))
			break
		}
	}

	return warnings
}

// ValidateForMode checks whether a command is allowed under the given
// permission mode. It returns an error describing why the command is blocked,
// or nil if the command is allowed.
func ValidateForMode(command string, mode permission.Mode) error {
	intent, reasons := ClassifyCommand(command)
	hasRedirects := DetectRedirects(command)
	dangerousPatterns := DetectDangerousPatterns(command)

	switch mode {
	case permission.ReadOnly:
		switch intent {
		case Write, Destructive, PackageManagement, ProcessManagement, SystemAdmin:
			return fmt.Errorf("command blocked in read-only mode: %s intent (%s)",
				intent, joinReasons(reasons))
		}
		if hasRedirects {
			return fmt.Errorf("command blocked in read-only mode: output redirects are not allowed")
		}

	case permission.WorkspaceWrite:
		switch intent {
		case Destructive:
			return fmt.Errorf("command blocked in workspace-write mode: destructive command (%s)",
				joinReasons(reasons))
		case ProcessManagement:
			return fmt.Errorf("command blocked in workspace-write mode: process management not allowed (%s)",
				joinReasons(reasons))
		case SystemAdmin:
			return fmt.Errorf("command blocked in workspace-write mode: system administration not allowed (%s)",
				joinReasons(reasons))
		}
		if len(dangerousPatterns) > 0 {
			return fmt.Errorf("command blocked in workspace-write mode: %s",
				strings.Join(dangerousPatterns, "; "))
		}

	case permission.DangerFullAccess:
		// Full access allows everything, but we still block obviously
		// catastrophic patterns like rm -rf /.
		for _, w := range dangerousPatterns {
			if strings.Contains(w, "remove root filesystem") {
				return fmt.Errorf("command blocked: %s", w)
			}
		}
	}

	return nil
}

func joinReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "no details"
	}
	return strings.Join(reasons, "; ")
}
