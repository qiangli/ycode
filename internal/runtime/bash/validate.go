package bash

import (
	"fmt"
	"strings"
)

// destructiveCommands that should trigger warnings.
var destructiveCommands = []string{
	"rm -rf",
	"rm -r",
	"git reset --hard",
	"git push --force",
	"git push -f",
	"git clean -f",
	"git checkout -- .",
	"git restore .",
	"drop table",
	"drop database",
	"truncate table",
	"kill -9",
	"pkill",
}

// readOnlyAllowed commands in read-only mode.
var readOnlyAllowed = []string{
	"cat", "head", "tail", "less", "more",
	"ls", "dir", "find", "grep", "rg", "ag",
	"wc", "sort", "uniq", "diff", "file", "stat",
	"git status", "git log", "git diff", "git show", "git branch",
	"echo", "printf", "date", "whoami", "hostname",
	"go version", "go env", "node --version", "python --version",
	"which", "where", "type", "command -v",
	"pwd", "env", "printenv",
}

// ValidateCommand checks if a command is safe to execute.
func ValidateCommand(command string, readOnly bool) error {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return fmt.Errorf("empty command")
	}

	if readOnly {
		return validateReadOnly(cmd)
	}

	return warnDestructive(cmd)
}

// validateReadOnly ensures only safe read-only commands are used.
func validateReadOnly(cmd string) error {
	lower := strings.ToLower(cmd)
	for _, allowed := range readOnlyAllowed {
		if strings.HasPrefix(lower, allowed) {
			return nil
		}
	}
	return fmt.Errorf("command not allowed in read-only mode: %s", cmd)
}

// warnDestructive returns an error for destructive commands that need confirmation.
func warnDestructive(cmd string) error {
	lower := strings.ToLower(cmd)
	for _, dangerous := range destructiveCommands {
		if strings.Contains(lower, dangerous) {
			return fmt.Errorf("destructive command detected: %s (contains %q)", cmd, dangerous)
		}
	}
	return nil
}
