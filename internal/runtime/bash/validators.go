package bash

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ValidationResult holds the outcome of a security validator.
type ValidationResult struct {
	ID      string // validator identifier (e.g., "V01")
	OK      bool
	Reason  string
	Command string
}

// Validator is a function that checks a command for a specific security concern.
type Validator func(command string) ValidationResult

// AllValidators returns all security validators in evaluation order.
// Inspired by Claude Code's bashSecurity.ts (20+ validators).
func AllValidators() []Validator {
	return []Validator{
		ValidateCommandSubstitution,
		ValidateProcessSubstitution,
		ValidateZshDangerous,
		ValidateIFSInjection,
		ValidateBlockedSleep,
		ValidateBlockedDevices,
		ValidateUnicodeControl,
		ValidateBraceExpansion,
		ValidateBacktickNesting,
		ValidateHeredocExpansion,
		ValidateSedInPlace,
		ValidateEvalExec,
	}
}

// RunAllValidators runs all security validators against a command.
// Returns the first failing result, or a passing result if all pass.
func RunAllValidators(command string) ValidationResult {
	for _, v := range AllValidators() {
		result := v(command)
		if !result.OK {
			return result
		}
	}
	return ValidationResult{OK: true}
}

// ValidateCommandSubstitution detects command substitution patterns that
// can be used to bypass security restrictions: $(), ${}, “.
func ValidateCommandSubstitution(command string) ValidationResult {
	// $() — command substitution.
	if strings.Contains(command, "$(") {
		return ValidationResult{
			ID:      "V01",
			OK:      false,
			Reason:  "command substitution via $() is not allowed",
			Command: command,
		}
	}

	// Backtick command substitution.
	if strings.Contains(command, "`") {
		return ValidationResult{
			ID:      "V01",
			OK:      false,
			Reason:  "command substitution via backticks is not allowed",
			Command: command,
		}
	}

	return ValidationResult{ID: "V01", OK: true}
}

// ValidateProcessSubstitution detects process substitution: <() and >().
func ValidateProcessSubstitution(command string) ValidationResult {
	if strings.Contains(command, "<(") || strings.Contains(command, ">(") {
		return ValidationResult{
			ID:      "V02",
			OK:      false,
			Reason:  "process substitution via <() or >() is not allowed",
			Command: command,
		}
	}
	return ValidationResult{ID: "V02", OK: true}
}

// zshDangerousCmds are zsh-specific commands that can bypass security.
var zshDangerousCmds = map[string]string{
	"zmodload": "module loading gateway",
	"sysopen":  "low-level file I/O bypass",
	"sysread":  "low-level file I/O bypass",
	"syswrite": "low-level file I/O bypass",
	"zpty":     "pseudo-terminal execution",
	"ztcp":     "network exfiltration",
	"zsocket":  "network exfiltration",
	"mapfile":  "array-based file I/O bypass",
}

// ValidateZshDangerous blocks zsh-specific dangerous commands.
func ValidateZshDangerous(command string) ValidationResult {
	for _, seg := range splitCommandSegments(command) {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		base := baseCommand(fields[0])
		if reason, ok := zshDangerousCmds[base]; ok {
			return ValidationResult{
				ID:      "V03",
				OK:      false,
				Reason:  fmt.Sprintf("zsh command %q blocked: %s", base, reason),
				Command: command,
			}
		}
	}
	return ValidationResult{ID: "V03", OK: true}
}

// ValidateIFSInjection detects IFS variable manipulation used to bypass
// command parsing. Setting IFS can change how the shell splits words.
func ValidateIFSInjection(command string) ValidationResult {
	if strings.Contains(command, "IFS=") {
		return ValidationResult{
			ID:      "V04",
			OK:      false,
			Reason:  "IFS variable manipulation is not allowed",
			Command: command,
		}
	}
	return ValidationResult{ID: "V04", OK: true}
}

// sleepPattern matches standalone sleep commands with duration >= 2s.
var sleepPattern = regexp.MustCompile(`(?:^|[;&|]\s*)sleep\s+(\d+)`)

// ValidateBlockedSleep blocks standalone sleep commands with duration >= 2s.
// Prevents the model from wasting time with unnecessary waits.
func ValidateBlockedSleep(command string) ValidationResult {
	matches := sleepPattern.FindStringSubmatch(command)
	if len(matches) >= 2 {
		seconds, err := strconv.Atoi(matches[1])
		if err == nil && seconds >= 2 {
			return ValidationResult{
				ID:      "V05",
				OK:      false,
				Reason:  fmt.Sprintf("sleep %ds is blocked (standalone sleep >= 2s not allowed)", seconds),
				Command: command,
			}
		}
	}
	return ValidationResult{ID: "V05", OK: true}
}

// blockedDevicePaths are device files that produce infinite or blocking output.
var blockedDevicePaths = map[string]bool{
	"/dev/zero":    true,
	"/dev/random":  true,
	"/dev/urandom": true,
	"/dev/full":    true,
	"/dev/stdin":   true,
	"/dev/tty":     true,
	"/dev/console": true,
}

// ValidateBlockedDevices blocks reads from infinite/blocking device files.
func ValidateBlockedDevices(command string) ValidationResult {
	for path := range blockedDevicePaths {
		if strings.Contains(command, path) {
			return ValidationResult{
				ID:      "V06",
				OK:      false,
				Reason:  fmt.Sprintf("access to device %s is blocked (infinite or blocking output)", path),
				Command: command,
			}
		}
	}
	// Block /proc/*/fd/0-2 (Linux stdio aliases).
	if matched, _ := regexp.MatchString(`/proc/\d+/fd/[012]`, command); matched {
		return ValidationResult{
			ID:      "V06",
			OK:      false,
			Reason:  "access to /proc/*/fd/0-2 (stdio aliases) is blocked",
			Command: command,
		}
	}
	return ValidationResult{ID: "V06", OK: true}
}

// ValidateUnicodeControl detects invisible Unicode control characters that
// can be used to disguise malicious commands (homoglyph/bidi attacks).
func ValidateUnicodeControl(command string) ValidationResult {
	for _, r := range command {
		// Zero-width characters.
		if r >= 0x200B && r <= 0x200F {
			return ValidationResult{
				ID:      "V07",
				OK:      false,
				Reason:  fmt.Sprintf("invisible Unicode character U+%04X detected (potential obfuscation)", r),
				Command: command,
			}
		}
		// Bidirectional control characters.
		if r >= 0x202A && r <= 0x202E {
			return ValidationResult{
				ID:      "V07",
				OK:      false,
				Reason:  fmt.Sprintf("bidirectional Unicode control U+%04X detected (potential display attack)", r),
				Command: command,
			}
		}
		// Other control characters (except common whitespace).
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return ValidationResult{
				ID:      "V07",
				OK:      false,
				Reason:  fmt.Sprintf("control character U+%04X detected", r),
				Command: command,
			}
		}
	}
	return ValidationResult{ID: "V07", OK: true}
}

// braceExpansionPattern detects {a..z} or {1..100} expansion patterns.
var braceExpansionPattern = regexp.MustCompile(`\{[^{}]*\.\.[^{}]*\}`)

// ValidateBraceExpansion detects brace expansion patterns that can be used
// to generate large argument lists or bypass restrictions.
func ValidateBraceExpansion(command string) ValidationResult {
	if braceExpansionPattern.MatchString(command) {
		return ValidationResult{
			ID:      "V08",
			OK:      false,
			Reason:  "brace expansion ({..}) detected — can generate large argument lists",
			Command: command,
		}
	}
	return ValidationResult{ID: "V08", OK: true}
}

// ValidateBacktickNesting detects nested backtick command substitution
// which can be used to build commands piecemeal to evade detection.
func ValidateBacktickNesting(command string) ValidationResult {
	depth := 0
	for _, r := range command {
		if r == '`' {
			depth++
		}
	}
	if depth >= 4 { // Nested backticks (2+ levels)
		return ValidationResult{
			ID:      "V09",
			OK:      false,
			Reason:  "deeply nested backtick substitution detected",
			Command: command,
		}
	}
	return ValidationResult{ID: "V09", OK: true}
}

// ValidateHeredocExpansion detects heredocs with variable expansion that
// could be used to construct dangerous commands.
func ValidateHeredocExpansion(command string) ValidationResult {
	// Heredoc without quoting the delimiter allows variable expansion.
	if strings.Contains(command, "<<") && !strings.Contains(command, "<<'") {
		// Only flag if there's also variable expansion in the heredoc.
		if strings.Contains(command, "${") || strings.Contains(command, "$((") {
			return ValidationResult{
				ID:      "V10",
				OK:      false,
				Reason:  "heredoc with variable expansion detected — use <<'EOF' to prevent expansion",
				Command: command,
			}
		}
	}
	return ValidationResult{ID: "V10", OK: true}
}

// sedInPlacePattern matches sed with -i flag.
var sedInPlacePattern = regexp.MustCompile(`(?:^|[;&|]\s*)sed\s+(?:.*\s)?-i`)

// ValidateSedInPlace detects sed -i (in-place edit) which bypasses file
// edit permission checks. These should go through the edit_file tool.
func ValidateSedInPlace(command string) ValidationResult {
	if sedInPlacePattern.MatchString(command) {
		return ValidationResult{
			ID:      "V11",
			OK:      false,
			Reason:  "sed -i (in-place edit) is blocked — use the edit_file tool instead to ensure permission checks",
			Command: command,
		}
	}
	return ValidationResult{ID: "V11", OK: true}
}

// ValidateEvalExec detects eval and exec which can construct and run
// arbitrary commands, potentially bypassing all other validators.
func ValidateEvalExec(command string) ValidationResult {
	for _, seg := range splitCommandSegments(command) {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		base := baseCommand(fields[0])
		if base == "eval" {
			return ValidationResult{
				ID:      "V12",
				OK:      false,
				Reason:  "eval is blocked — it can construct arbitrary commands that bypass security checks",
				Command: command,
			}
		}
	}
	return ValidationResult{ID: "V12", OK: true}
}
