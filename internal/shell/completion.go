package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CompletionKind tells the caller which namespace a completion came from.
type CompletionKind int

const (
	// CompletionPATH is a binary name found on $PATH.
	CompletionPATH CompletionKind = iota
	// CompletionSlash is a slash-command from the curated shell-safe set.
	CompletionSlash
	// CompletionSkill is a skill from SkillResolver.List().
	CompletionSkill
)

// String returns a human-readable label.
func (k CompletionKind) String() string {
	switch k {
	case CompletionPATH:
		return "path"
	case CompletionSlash:
		return "slash"
	case CompletionSkill:
		return "skill"
	}
	return "unknown"
}

// Completion is a single completion candidate.
type Completion struct {
	Kind CompletionKind
	// Display is the string shown to the user (with sentinel).
	Display string
	// Replacement is what should replace the current word in the input.
	Replacement string
}

// CompleteFor returns completion candidates for the given prefix.
//
// Rules (plan §12c — namespace-aware Tab):
//   - prefix `^/` → curated shell-safe slash commands
//   - prefix `^@` → skills from SkillResolver.List()
//   - bare        → first binary on $PATH whose basename starts with prefix.
//     PATH walking is bounded to the first match-page (max 32) per dir to
//     keep completion snappy.
//
// If `rt` is nil only the built-in slash and PATH completions are tried.
func CompleteFor(rt *ShellRuntime, prefix string) []Completion {
	prefix = strings.TrimLeft(prefix, " \t")
	if prefix == "" {
		return nil
	}
	switch prefix[0] {
	case '/':
		return completeSlash(rt, prefix[1:])
	case '@':
		if rt == nil || rt.Skills() == nil {
			return nil
		}
		return completeSkill(rt.Skills().List(), prefix[1:])
	default:
		return completePATH(prefix)
	}
}

func completeSlash(rt *ShellRuntime, after string) []Completion {
	if rt == nil || rt.Registry() == nil {
		return nil
	}
	specs := rt.Registry().List()
	out := make([]Completion, 0, len(specs))
	for _, spec := range specs {
		if !spec.ShellSafe {
			continue
		}
		if strings.HasPrefix(spec.Name, after) {
			out = append(out, Completion{
				Kind:        CompletionSlash,
				Display:     "/" + spec.Name,
				Replacement: "/" + spec.Name,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Display < out[j].Display })
	return out
}

func completeSkill(skills []string, after string) []Completion {
	out := make([]Completion, 0, len(skills))
	for _, name := range skills {
		if strings.HasPrefix(name, after) {
			out = append(out, Completion{
				Kind:        CompletionSkill,
				Display:     "@" + name,
				Replacement: "@" + name,
			})
		}
	}
	return out
}

func completePATH(prefix string) []Completion {
	const perDirLimit = 32
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []Completion

	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		count := 0
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, Completion{
				Kind:        CompletionPATH,
				Display:     name,
				Replacement: name,
			})
			count++
			if count >= perDirLimit {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Display < out[j].Display })
	return out
}

// FormatCompletions renders a list of candidates one per line, with a
// short "kind" tag prefix. Used by the TUI Tab handler to dump candidates
// into the scroll history.
func FormatCompletions(cs []Completion) string {
	if len(cs) == 0 {
		return "(no completions)"
	}
	var sb strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&sb, "  %-8s %s\n", c.Kind.String(), c.Display)
	}
	return strings.TrimRight(sb.String(), "\n")
}
