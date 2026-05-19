package agentmode

import (
	"os"
	"regexp"
	"strings"
)

// dangerPatterns is the regex set that triggers auto-sandboxing when
// $YCODE_AUTO_SANDBOX=1 is set. Each pattern carries a short reason
// shown to the user on stderr so the rewrite isn't surprising.
//
// Patterns intentionally err on the side of false positives — if a user
// opts in, they're saying "I'd rather over-sandbox than miss a footgun."
var dangerPatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`\brm\b\s+(-rf|-fr|-r\s+-f|-f\s+-r|-Rf|-fR)\b`), "recursive force delete"},
	{regexp.MustCompile(`\bmake\b\s+\w*(clean|distclean|maintainer-clean)\b`), "make clean target"},
	{regexp.MustCompile(`\bnpm\s+install\b`), "npm install (runs lifecycle scripts)"},
	{regexp.MustCompile(`\byarn\s+(install|add)\b`), "yarn install/add"},
	{regexp.MustCompile(`\bpnpm\s+(install|add)\b`), "pnpm install/add"},
	{regexp.MustCompile(`\bpip\s+install\b`), "pip install"},
	{regexp.MustCompile(`\b(curl|wget)\b[^|]+\|\s*(sh|bash|zsh)\b`), "curl|sh remote-script exec"},
	{regexp.MustCompile(`\bgit\s+clean\b[^|]*-\w*[fd]\w*\b`), "git clean -fd"},
	{regexp.MustCompile(`\bdd\b[^|]*\bof=`), "dd with output file"},
	{regexp.MustCompile(`\bchmod\b\s+-R\b`), "recursive chmod"},
	{regexp.MustCompile(`\bchown\b\s+-R\b`), "recursive chown"},
}

// MaybeAutoSandbox returns a rewritten command (and the reason it was
// rewritten) when:
//   - $YCODE_AUTO_SANDBOX is set to a truthy value, AND
//   - the command matches a danger pattern, AND
//   - the command does NOT contain the `--no-sandbox` opt-out flag.
//
// Returns ("", "") when no rewrite applies; the caller runs the original
// command unchanged.
//
// The rewrite shape is `yc sandbox -- <orig>`. Because `yc sandbox` is a
// builtin intercepted by the bash exec middleware, this works wherever
// the dispatcher's bash interpreter has the builtins.Handler wired in.
func MaybeAutoSandbox(command string) (rewritten, reason string) {
	if !autoSandboxEnabled() {
		return "", ""
	}
	if strings.Contains(command, "--no-sandbox") {
		return "", ""
	}
	for _, p := range dangerPatterns {
		if p.pattern.MatchString(command) {
			// Use single-quote-wrap-with-escape so the original command's
			// quoting survives going through `yc sandbox --`. Alpine ships
			// /bin/sh (ash), not bash — use sh so the auto-sandbox path
			// works with the default sandbox image.
			return "yc sandbox -- sh -c " + shellQuote(command), p.reason
		}
	}
	return "", ""
}

// autoSandboxEnabled reports whether the env var is set to a truthy
// value. Accepts 1, true, yes (case-insensitive). Anything else,
// including unset, returns false.
func autoSandboxEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("YCODE_AUTO_SANDBOX")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// shellQuote wraps a string in single quotes for safe passage as a
// single argv to `bash -c`. Inner single quotes are escaped by the
// classic '\” trick: close-quote, escaped-quote, reopen-quote.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
