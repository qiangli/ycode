package tools

// RegisterBuiltinsFiltered registers only the built-in tool specs whose names
// appear in allowed. Pass an empty slice to register nothing; pass nil to fall
// through to RegisterBuiltins semantics (everything).
//
// This is the seam that lets a pkg/ycode host opt out of dangerous defaults
// (bash, write_file, edit_file, Agent) without losing the ability to keep a
// curated subset of read-only tools.
func RegisterBuiltinsFiltered(r *Registry, allowed []string) {
	if allowed == nil {
		RegisterBuiltins(r)
		return
	}
	allow := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allow[name] = true
	}
	for _, spec := range builtinSpecs() {
		if !allow[spec.Name] {
			continue
		}
		_ = r.Register(spec)
	}
}

// RegisterBuiltinsExcluding registers every built-in tool whose name is NOT
// in blocked. Pass nil or an empty slice to register everything (equivalent
// to RegisterBuiltins).
//
// Used by `ycode serve --tools-blocklist=name1,name2` for operator-level
// restriction in shared-tenant deployments.
func RegisterBuiltinsExcluding(r *Registry, blocked []string) {
	if len(blocked) == 0 {
		RegisterBuiltins(r)
		return
	}
	deny := make(map[string]bool, len(blocked))
	for _, name := range blocked {
		deny[name] = true
	}
	for _, spec := range builtinSpecs() {
		if deny[spec.Name] {
			continue
		}
		_ = r.Register(spec)
	}
}
