package spec

import (
	"fmt"
	"sort"
	"strings"
)

// The bashy-kb provider is the only compiled memory backend: recall and
// persist are harness-authored bashy.run nodes over `bashy kb`, so the memory
// resource is pure policy (rings, forms, budgets, cadence) with no Go-side
// store behind it. The former memex provider value is rejected; the harness
// no longer opens memex.
const MemoryProviderBashyKB = "bashy-kb"

var memoryRings = map[string]struct{}{"agent": {}, "repo": {}, "host": {}}
var memoryForms = map[string]struct{}{"note": {}, "page": {}, "relation": {}, "code": {}}

func validateMemories(memories map[string]Memory) error {
	names := make([]string, 0, len(memories))
	for name := range memories {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := validateMemory(memories[name]); err != nil {
			return fmt.Errorf("harness: spec.memories.%s: %w", name, err)
		}
	}
	return nil
}

func validateMemory(memory Memory) error {
	if memory.Provider != MemoryProviderBashyKB {
		return fmt.Errorf("provider %q is not supported; the only compiled memory provider is %q", memory.Provider, MemoryProviderBashyKB)
	}
	if err := validateFacet("recall.rings", memory.Recall.Rings, memoryRings); err != nil {
		return err
	}
	if err := validateFacet("recall.forms", memory.Recall.Forms, memoryForms); err != nil {
		return err
	}
	if memory.Recall.MaxItems <= 0 {
		return fmt.Errorf("recall.maxItems must be positive")
	}
	if memory.Recall.MaxTokens <= 0 {
		return fmt.Errorf("recall.maxTokens must be positive")
	}
	// The Persist stage runs once per compiled turn; without a session turn
	// counter no mechanism can honor a longer cadence, so anything but 1 is
	// rejected instead of silently written every turn.
	if memory.Write.EveryTurns != 1 {
		return fmt.Errorf("write.everyTurns %d is not supported: the persist node runs once per turn, so the only honest cadence is 1", memory.Write.EveryTurns)
	}
	return nil
}

func validateFacet(path string, values []string, allowed map[string]struct{}) error {
	if len(values) == 0 {
		return fmt.Errorf("%s must name at least one value", path)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			known := make([]string, 0, len(allowed))
			for name := range allowed {
				known = append(known, name)
			}
			sort.Strings(known)
			return fmt.Errorf("%s value %q is not one of %s", path, value, strings.Join(known, ", "))
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("%s repeats %q", path, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}
