package bash

import (
	"fmt"
	"regexp"
	"strings"
)

// commonAlternatives maps commands to their common alternative names.
var commonAlternatives = map[string][]string{
	"python":    {"python3", "python3.12", "python3.11", "python3.10"},
	"python3":   {"python"},
	"pip":       {"pip3"},
	"pip3":      {"pip"},
	"node":      {"nodejs"},
	"nodejs":    {"node"},
	"cc":        {"gcc", "clang"},
	"gcc":       {"cc", "clang"},
	"g++":       {"clang++", "c++"},
	"clang":     {"gcc", "cc"},
	"clang++":   {"g++", "c++"},
	"make":      {"gmake"},
	"gmake":     {"make"},
	"vim":       {"vi", "nvim"},
	"vi":        {"vim", "nvim"},
	"gawk":      {"awk", "mawk"},
	"awk":       {"gawk", "mawk"},
	"sed":       {"gsed"},
	"gsed":      {"sed"},
	"grep":      {"ggrep"},
	"find":      {"gfind", "fd"},
	"sha256sum": {"shasum -a 256"},
	"md5sum":    {"md5"},
	"md5":       {"md5sum"},
	"realpath":  {"readlink -f"},
	"readlink":  {"realpath"},
	"nc":        {"ncat", "netcat"},
	"netcat":    {"nc", "ncat"},
	"docker":    {"podman"},
	"podman":    {"docker"},
	"apt":       {"apt-get"},
	"apt-get":   {"apt"},
	"yum":       {"dnf"},
	"dnf":       {"yum"},
}

// commandNotFoundRE matches common "command not found" error messages.
var commandNotFoundRE = regexp.MustCompile(
	`(?:bash: |sh: |zsh: )?(?:line \d+: )?(\S+): (?:command not found|not found)`,
)

// SuggestAlternatives analyzes a "command not found" error and returns
// helpful suggestions. Returns empty string if no suggestions available.
func SuggestAlternatives(stderr string) string {
	matches := commandNotFoundRE.FindStringSubmatch(stderr)
	if len(matches) < 2 {
		return ""
	}
	missingCmd := matches[1]

	var suggestions []string

	// Check known alternatives.
	if alts, ok := commonAlternatives[missingCmd]; ok {
		suggestions = append(suggestions, alts...)
	}

	if len(suggestions) == 0 {
		return fmt.Sprintf("Hint: %q not found. Try installing it or check if it's available under a different name.", missingCmd)
	}

	return fmt.Sprintf("Hint: %q not found. Try: %s", missingCmd, strings.Join(suggestions, ", "))
}
