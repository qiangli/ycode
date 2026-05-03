package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// TeamMemorySubdir is the subdirectory name for team-shared memories.
	TeamMemorySubdir = "team"

	// MaxTeamMemoryFiles caps the number of team memory files to scan.
	MaxTeamMemoryFiles = 200

	// MaxTeamMemoryBytes caps the size of team MEMORY.md index.
	MaxTeamMemoryBytes = 25_000
)

// TeamMemoryPaths manages the parallel directory structure for team memory.
// Team memories are stored alongside private memories in a 'team/' subdirectory.
//
// Inspired by Claude Code's teamMemPaths.ts.
type TeamMemoryPaths struct {
	// PrivateDir is the root directory for private memories.
	PrivateDir string
	// TeamDir is the root directory for team memories (PrivateDir/team/).
	TeamDir string
}

// NewTeamMemoryPaths creates team memory paths from a private memory directory.
func NewTeamMemoryPaths(privateDir string) *TeamMemoryPaths {
	return &TeamMemoryPaths{
		PrivateDir: privateDir,
		TeamDir:    filepath.Join(privateDir, TeamMemorySubdir),
	}
}

// EnsureTeamDir creates the team memory directory if it doesn't exist.
func (tmp *TeamMemoryPaths) EnsureTeamDir() error {
	return os.MkdirAll(tmp.TeamDir, 0o755)
}

// ValidateTeamPath checks that a file path is safe to use within the team
// memory directory. It resolves symlinks and detects path traversal attempts.
//
// Returns the resolved path and an error if the path is unsafe.
func (tmp *TeamMemoryPaths) ValidateTeamPath(path string) (string, error) {
	// Resolve the team directory to its real path.
	realTeamDir, err := filepath.EvalSymlinks(tmp.TeamDir)
	if err != nil {
		// Team dir doesn't exist yet — use the literal path.
		realTeamDir = tmp.TeamDir
	}

	// Resolve the target path.
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		// File doesn't exist yet — use the literal path but resolve parent.
		dir := filepath.Dir(path)
		resolvedDir, dirErr := filepath.EvalSymlinks(dir)
		if dirErr != nil {
			resolvedDir = dir
		}
		resolvedPath = filepath.Join(resolvedDir, filepath.Base(path))
	}

	// Check that the resolved path is within the team directory.
	if !strings.HasPrefix(resolvedPath, realTeamDir+string(filepath.Separator)) &&
		resolvedPath != realTeamDir {
		return "", fmt.Errorf("path traversal detected: %s resolves outside team directory %s", path, realTeamDir)
	}

	return resolvedPath, nil
}

// IsTeamSafe checks whether content is safe to store in team memory.
// Rejects content that appears to contain secrets or credentials.
func IsTeamSafe(content string) bool {
	secretPatterns := []string{
		"sk-ant-", "sk-", "api_key", "apikey", "API_KEY",
		"password", "passwd", "secret", "token",
		"private_key", "PRIVATE KEY",
		"credential", "auth_token",
	}

	lowered := strings.ToLower(content)
	for _, pattern := range secretPatterns {
		if strings.Contains(lowered, strings.ToLower(pattern)) {
			// Check if it's in a code context (assignment, not just mention).
			idx := strings.Index(lowered, strings.ToLower(pattern))
			// Look for assignment-like patterns around the match.
			surrounding := lowered[max(0, idx-20):min(len(lowered), idx+len(pattern)+20)]
			if strings.ContainsAny(surrounding, "=:") {
				return false
			}
		}
	}
	return true
}

// MemoryStaleness provides a staleness assessment for a memory.
type MemoryStaleness struct {
	// AgeDays is the age of the memory in days (0 = today).
	AgeDays int
	// IsStale is true if the memory is older than 1 day.
	IsStale bool
	// CaveatText is the staleness warning text for prompt injection.
	CaveatText string
}

// AssessMemoryStaleness evaluates the staleness of a memory and produces
// appropriate caveat text for prompt injection.
//
// Inspired by Claude Code's memoryAge.ts which adds explicit staleness
// warnings for memories older than 1 day.
func AssessMemoryStaleness(ageDays int) MemoryStaleness {
	if ageDays <= 0 {
		return MemoryStaleness{AgeDays: 0, IsStale: false}
	}

	caveat := fmt.Sprintf("This memory was saved %s — verify before acting on it.", formatAge(ageDays))

	return MemoryStaleness{
		AgeDays:    ageDays,
		IsStale:    true,
		CaveatText: caveat,
	}
}
