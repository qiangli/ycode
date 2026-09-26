package ycodecli

import (
	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
	"strings"
)

func loadHarness(file string) (*harnessspec.Document, error) { return harnessspec.Load(file) }

func getDotted(values map[string]any, key string) (any, bool) {
	var current any = values
	for _, part := range strings.Split(key, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
