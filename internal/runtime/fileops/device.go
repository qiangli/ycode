package fileops

import (
	"fmt"
	"regexp"
	"strings"
)

// blockedDevicePaths are device files that produce infinite or blocking output.
// Reading from these would hang or exhaust memory.
// Inspired by Claude Code's FileReadTool blocked device paths.
var blockedDevicePaths = map[string]bool{
	"/dev/zero":    true,
	"/dev/random":  true,
	"/dev/urandom": true,
	"/dev/full":    true,
	"/dev/stdin":   true,
	"/dev/tty":     true,
	"/dev/console": true,
	"/dev/stdout":  true,
	"/dev/stderr":  true,
}

// procFDPattern matches Linux stdio aliases under /proc/.
var procFDPattern = regexp.MustCompile(`^/proc/\d+/fd/[012]$`)

// ValidateReadPath checks whether a file path is safe to read.
// Returns an error if the path points to a blocked device or system file.
func ValidateReadPath(path string) error {
	if blockedDevicePaths[path] {
		return fmt.Errorf("cannot read %s: device file produces infinite or blocking output", path)
	}

	// Block /proc/*/fd/0-2 (stdio aliases).
	if procFDPattern.MatchString(path) {
		return fmt.Errorf("cannot read %s: stdio device alias", path)
	}

	// Block /dev/ paths in general (less common but still dangerous).
	if strings.HasPrefix(path, "/dev/") && !isAllowedDevicePath(path) {
		return fmt.Errorf("cannot read %s: device files are not allowed", path)
	}

	return nil
}

// isAllowedDevicePath returns true for /dev/ paths that are safe to read.
func isAllowedDevicePath(path string) bool {
	allowed := map[string]bool{
		"/dev/null": true,
	}
	return allowed[path]
}
