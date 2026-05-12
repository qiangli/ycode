package wrap

import "os"

// applyRuntimeOverrides merges the sentinel-keyed map returned by
// hookruntime.Materialize into the wrapped agent's env. The two
// sentinels it knows:
//
//   - __PYTHONPATH_PREPEND__ — prepend the value to PYTHONPATH so the
//     hooked sitecustomize.py wins over user-installed ones.
//   - __NODE_OPTIONS_APPEND__ — append the value to NODE_OPTIONS so
//     existing flags (e.g. --enable-source-maps) survive.
//
// Both sentinels coalesce against any value the wrapped agent had in
// its inherited env. Unknown keys (forward-compat for future runtimes)
// are written verbatim.
func applyRuntimeOverrides(env []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return env
	}
	getOrig := func(key string) string { return extractEnv(env, key) }

	if v, ok := overrides["__PYTHONPATH_PREPEND__"]; ok {
		orig := getOrig("PYTHONPATH")
		if orig == "" {
			env = setEnv(env, "PYTHONPATH", v)
		} else {
			env = setEnv(env, "PYTHONPATH", v+string(os.PathListSeparator)+orig)
		}
		delete(overrides, "__PYTHONPATH_PREPEND__")
	}
	if v, ok := overrides["__NODE_OPTIONS_APPEND__"]; ok {
		orig := getOrig("NODE_OPTIONS")
		if orig == "" {
			env = setEnv(env, "NODE_OPTIONS", v)
		} else {
			env = setEnv(env, "NODE_OPTIONS", orig+" "+v)
		}
		delete(overrides, "__NODE_OPTIONS_APPEND__")
	}
	for k, v := range overrides {
		env = setEnv(env, k, v)
	}
	return env
}

// setEnv replaces or appends key=value in env. Pre-existing entries
// for the same key are overwritten in place (preserves slice ordering
// so PATH/SHELL placement stays stable in `ps` listings).
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, kv := range env {
		if len(kv) >= len(prefix) && kv[:len(prefix)] == prefix {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
