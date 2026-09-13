package turn

import (
	"fmt"
	"regexp"
	"strings"
)

var bashyRunTemplatePattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// resolveBashyRunScript substitutes the compiled script's remaining
// placeholders — {{session}} and {{<port>}} — as single-quoted shell
// literals before the script is composed, preflighted and digest-bound.
// Literals are the only provable path into a command argument: the Bashy
// intent analyzer treats a variable expansion in an argument as unprovable
// and the harness denies incomplete evidence, so a template never falls back
// to $YCODE_IN_<PORT>. A placeholder naming a missing or non-scalar port
// fails the stage closed.
func resolveBashyRunScript(script, sessionID string, inputs map[string]any) (string, error) {
	var resolveErr error
	resolved := bashyRunTemplatePattern.ReplaceAllStringFunc(script, func(match string) string {
		if resolveErr != nil {
			return match
		}
		name := strings.TrimSpace(match[2 : len(match)-2])
		if name == "session" {
			return shellSingleQuote(sessionID)
		}
		value, declared := inputs[name]
		if !declared {
			resolveErr = fmt.Errorf("bashy.run: script placeholder {{%s}} has no bound input", name)
			return match
		}
		literal, scalar := scalarEnvValue(value)
		if !scalar {
			resolveErr = fmt.Errorf("bashy.run: script placeholder {{%s}} requires a scalar input, got %T", name, value)
			return match
		}
		return shellSingleQuote(literal)
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return resolved, nil
}
