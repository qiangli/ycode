package fileops

import (
	"path/filepath"
	"strings"
)

// SensitiveFileAction indicates what action to take for a file.
type SensitiveFileAction int

const (
	// FileAllowed means the file can be freely accessed.
	FileAllowed SensitiveFileAction = iota
	// FileAskUser means the file needs user confirmation before access.
	FileAskUser
	// FileBlocked means the file should not be read or written.
	FileBlocked
)

// safeEnvSuffixes are .env.* suffixes that are always allowed.
var safeEnvSuffixes = []string{".example", ".sample", ".template"}

// credentialPatterns are glob patterns for credential files.
var credentialPatterns = []string{
	"credentials.json",
	"*.pem",
	"*.key",
	"id_rsa*",
	"*.pfx",
}

// CheckSensitiveFile checks if a file path is sensitive and returns the
// recommended action. Files like .env are flagged as needing user confirmation,
// while .env.example is always allowed.
func CheckSensitiveFile(path string) SensitiveFileAction {
	base := filepath.Base(path)
	lower := strings.ToLower(base)

	// Check .env files.
	if lower == ".env" || strings.HasPrefix(lower, ".env.") {
		// Allow safe variants.
		for _, suffix := range safeEnvSuffixes {
			if strings.HasSuffix(lower, suffix) {
				return FileAllowed
			}
		}
		return FileAskUser
	}

	// Check credential patterns.
	for _, pattern := range credentialPatterns {
		if matched, _ := filepath.Match(pattern, base); matched {
			return FileAskUser
		}
		// Also try case-insensitive match.
		if matched, _ := filepath.Match(strings.ToLower(pattern), lower); matched {
			return FileAskUser
		}
	}

	return FileAllowed
}
