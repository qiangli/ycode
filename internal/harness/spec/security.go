package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
)

type resourceNode struct {
	value    reflect.Value
	exported bool
}

func validateReachability(d *Document) error {
	resources := make(map[string]resourceNode)
	specValue, specType := reflect.ValueOf(d.Spec), reflect.TypeOf(d.Spec)
	for i := 0; i < specValue.NumField(); i++ {
		field, values := specType.Field(i), specValue.Field(i)
		kind := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if values.Kind() != reflect.Map || kind == "extensions" || kind == "imports" {
			continue
		}
		for _, key := range values.MapKeys() {
			value := values.MapIndex(key)
			resources[kind+"/"+key.String()] = resourceNode{value: value, exported: exportedValue(value)}
		}
	}
	queue := []string{"agents/" + d.Spec.Runtime.DefaultAgentRef}
	queue = append(queue, collectResourceRefs(reflect.ValueOf(d.Spec.Interfaces))...)
	for id, node := range resources {
		if strings.HasPrefix(id, "triggers/") || node.exported {
			queue = append(queue, id)
		}
	}
	reached := make(map[string]bool)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if reached[id] {
			continue
		}
		reached[id] = true
		node, ok := resources[id]
		if !ok {
			continue
		}
		refs := collectResourceRefs(node.value)
		for _, ref := range refs {
			queue = append(queue, ref)
		}
	}
	var unused []string
	for id, node := range resources {
		if !reached[id] && !node.exported {
			unused = append(unused, id)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		return fmt.Errorf("harness: unreachable resources must declare exported: true: %s", strings.Join(unused, ", "))
	}
	return nil
}

func exportedValue(v reflect.Value) bool {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	field := v.FieldByName("Exported")
	return field.IsValid() && field.Kind() == reflect.Bool && field.Bool()
}

func collectResourceRefs(v reflect.Value) []string {
	var refs []string
	var walk func(reflect.Value)
	walk = func(value reflect.Value) {
		for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
			if value.IsNil() {
				return
			}
			value = value.Elem()
		}
		switch value.Kind() {
		case reflect.Struct:
			t := value.Type()
			for i := 0; i < value.NumField(); i++ {
				field := t.Field(i)
				name := strings.Split(field.Tag.Get("yaml"), ",")[0]
				if name == "" || name == "-" {
					continue
				}
				if name == "hook.invoke" {
					if value.Field(i).String() != "" {
						refs = append(refs, "hooks/"+value.Field(i).String())
					}
					continue
				}
				if kind := resourceKindForRef(name); kind != "" {
					for _, item := range referenceStrings(value.Field(i)) {
						refs = append(refs, kind+"/"+item)
					}
					continue
				}
				walk(value.Field(i))
			}
		case reflect.Map:
			for _, key := range value.MapKeys() {
				if key.Kind() == reflect.String {
					if kind := resourceKindForRef(key.String()); kind != "" {
						for _, item := range referenceStrings(value.MapIndex(key)) {
							refs = append(refs, kind+"/"+item)
						}
						continue
					}
				}
				walk(value.MapIndex(key))
			}
		case reflect.Slice:
			for i := 0; i < value.Len(); i++ {
				walk(value.Index(i))
			}
		}
	}
	walk(v)
	return refs
}

func resourceKindForRef(name string) string {
	if !strings.HasSuffix(name, "Ref") && !strings.HasSuffix(name, "Refs") {
		return ""
	}
	base := strings.TrimSuffix(strings.TrimSuffix(name, "Refs"), "Ref")
	switch base {
	case "defaultAgent", "agent":
		return "agents"
	case "provider":
		return "providers"
	case "model":
		return "models"
	case "modelRoute", "route":
		return "routes"
	case "pipeline", "defaultPipeline":
		return "pipelines"
	case "source", "instruction":
		return "sources"
	case "context":
		return "contexts"
	case "memory":
		return "memories"
	case "queue":
		return "queues"
	case "session":
		return "sessions"
	case "lifecycle":
		return "lifecycles"
	case "policy":
		return "policies"
	case "placement":
		return "placements"
	case "lock":
		return "locks"
	case "hook":
		return "hooks"
	case "skill":
		return "skills"
	case "frontend", "terminalFrontend":
		return "frontends"
	case "trigger":
		return "triggers"
	case "sink":
		return "sinks"
	default:
		return ""
	}
}

func referenceStrings(v reflect.Value) []string {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.String {
		if v.String() != "" {
			return []string{v.String()}
		}
		return nil
	}
	if v.Kind() != reflect.Slice {
		return nil
	}
	out := make([]string, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		item := referenceStrings(v.Index(i))
		out = append(out, item...)
	}
	return out
}

func validateControlRoot(d *Document) error {
	if err := validateControlRootAt(d.Spec.Runtime, d.Spec.Placements, d.BaseDir, ControlBase()); err != nil {
		return err
	}
	for name, session := range d.Spec.Sessions {
		mode := session.Events.Permissions
		if mode.Directory&0o077 != 0 || mode.File&0o077 != 0 || mode.Directory == 0 || mode.File == 0 {
			return fmt.Errorf("harness: session %q control storage permissions must be private and nonzero", name)
		}
	}
	return nil
}

func validateControlRootAt(runtime Runtime, placements map[string]Placement, documentDir, platformDir string) error {
	name := runtime.ControlRoot.PlatformDataDir
	if name == "" || filepath.IsAbs(name) || filepath.Clean(name) == "." || strings.HasPrefix(filepath.Clean(name), ".."+string(filepath.Separator)) {
		return errors.New("harness: runtime.controlRoot.platformDataDir must be a non-empty relative private path")
	}
	if runtime.RootOverlap != "deny" {
		return errors.New("harness: runtime.rootOverlap must be deny")
	}
	control := filepath.Join(platformDir, filepath.Clean(name))
	if err := rejectSymlinkComponents(platformDir, control); err != nil {
		return fmt.Errorf("harness: runtime.controlRoot: %w", err)
	}
	if info, err := os.Stat(control); err == nil && unixModeBits && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("harness: runtime.controlRoot %q must have private permissions, got %04o", control, info.Mode().Perm())
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("harness: runtime.controlRoot: %w", err)
	}
	modelRoots := append(append([]string{}, runtime.ReadableRoots...), runtime.WritableRoots...)
	for _, placement := range placements {
		for _, mount := range placement.Mounts {
			if mount == "workspace" {
				modelRoots = append(modelRoots, runtime.Workspace)
			} else {
				modelRoots = append(modelRoots, mount)
			}
		}
	}
	for _, root := range modelRoots {
		if !filepath.IsAbs(root) {
			root = filepath.Join(documentDir, root)
		}
		if pathsOverlap(control, root) {
			return fmt.Errorf("harness: runtime.controlRoot %q overlaps model-visible root %q", control, root)
		}
	}
	return nil
}

func rejectSymlinkComponents(base, target string) error {
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("escapes platform data directory")
	}
	current := base
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %q is a symlink", current)
		}
	}
	return nil
}

func pathsOverlap(a, b string) bool {
	a, b = canonicalPath(a), canonicalPath(b)
	relAB, errAB := filepath.Rel(a, b)
	relBA, errBA := filepath.Rel(b, a)
	return (errAB == nil && relAB != ".." && !strings.HasPrefix(relAB, ".."+string(filepath.Separator))) || (errBA == nil && relBA != ".." && !strings.HasPrefix(relBA, ".."+string(filepath.Separator)))
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(filepath.Clean(path)); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func validateAuthority(d *Document) error {
	bashy := d.Spec.Bashy.Execution
	if _, err := permissionRank(bashy.PermissionCeiling); err != nil {
		return fmt.Errorf("harness: spec.bashy: %w", err)
	}
	maxEffects, err := effectSet("spec.bashy.execution.effectsCeiling", bashy.EffectsCeiling)
	if err != nil {
		return err
	}
	for name, agent := range d.Spec.Agents {
		if err := permissionAtMost("agent "+name, agent.PermissionCeiling, bashy.PermissionCeiling); err != nil {
			return err
		}
		agentEffects, err := effectSet("spec.agents."+name+".effectsCeiling", agent.EffectsCeiling)
		if err != nil {
			return err
		}
		if err := subsetEffects("agent "+name, agentEffects, maxEffects); err != nil {
			return err
		}
		for _, grant := range agent.Delegations {
			target := d.Spec.Agents[grant.AgentRef]
			if err := permissionAtMost("delegation "+name+"->"+grant.AgentRef, grant.PermissionCeiling, agent.PermissionCeiling); err != nil {
				return err
			}
			if err := permissionAtMost("delegation "+name+"->"+grant.AgentRef, grant.PermissionCeiling, target.PermissionCeiling); err != nil {
				return err
			}
			grantEffects, err := effectSet("delegation "+name+"->"+grant.AgentRef, grant.EffectsCeiling)
			if err != nil {
				return err
			}
			if err := subsetEffects("delegation "+name+"->"+grant.AgentRef, grantEffects, agentEffects); err != nil {
				return err
			}
			targetEffects, _ := effectSet("target", target.EffectsCeiling)
			if err := subsetEffects("delegation "+name+"->"+grant.AgentRef, grantEffects, targetEffects); err != nil {
				return err
			}
			if grant.MaxParallel <= 0 || grant.MaxParallel > bashy.MaxParallel {
				return fmt.Errorf("harness: delegation %s->%s maxParallel %d exceeds Bashy ceiling %d", name, grant.AgentRef, grant.MaxParallel, bashy.MaxParallel)
			}
			if route, ok := d.Spec.Routes[grant.ModelRouteRef]; ok {
				if err := budgetAtMost(name+"->"+grant.AgentRef, grant.Budget, route.Budget); err != nil {
					return err
				}
			}
		}
	}
	for name, policy := range d.Spec.Policies {
		for _, rule := range policy.Rules {
			if rule.Decision == "allow" {
				effects, err := effectSet("policy "+name+" rule "+rule.ID, rule.Match.EffectsAllWithin)
				if err != nil {
					return err
				}
				if err := subsetEffects("policy "+name+" rule "+rule.ID, effects, maxEffects); err != nil {
					return err
				}
			}
		}
	}
	for name, placement := range d.Spec.Placements {
		for _, mount := range placement.Mounts {
			if mount != bashy.CWDRoot {
				return fmt.Errorf("harness: placement %q mount %q exceeds Bashy cwdRoot %q", name, mount, bashy.CWDRoot)
			}
		}
	}
	return nil
}

func validatePlatformCompatibility(d *Document) error {
	host := runtime.GOOS + "-" + runtime.GOARCH
	for name, placement := range d.Spec.Placements {
		if placement.UnsupportedEffect != "reject" {
			return fmt.Errorf("harness: placement %q unsupportedEffect must be reject", name)
		}
		seen, supported := make(map[string]bool), false
		for _, platform := range placement.SupportedPlatforms {
			parts := strings.Split(platform, "-")
			if len(parts) != 2 || !knownOS(parts[0]) || !knownArch(parts[1]) {
				return fmt.Errorf("harness: placement %q has unsupported platform %q", name, platform)
			}
			if seen[platform] {
				return fmt.Errorf("harness: placement %q repeats platform %q", name, platform)
			}
			seen[platform] = true
			if platform == host {
				supported = true
			}
		}
		if !supported {
			return fmt.Errorf("harness: placement %q does not support current platform %q", name, host)
		}
	}
	if d.Spec.Bashy.Execution.IncompletePreflight != "deny" || d.Spec.Bashy.Execution.HostFallback != "deny" {
		return errors.New("harness: Bashy must reject incomplete preflight and host fallback")
	}
	for name, policy := range d.Spec.Policies {
		if policy.IncompletePreflight != "deny" {
			return fmt.Errorf("harness: policy %q must deny incomplete preflight", name)
		}
	}
	return nil
}
func knownOS(value string) bool   { return value == "darwin" || value == "linux" || value == "windows" }
func knownArch(value string) bool { return value == "amd64" || value == "arm64" }

func permissionRank(value string) (int, error) {
	switch value {
	case "read-only":
		return 0, nil
	case "workspace-write":
		return 1, nil
	case "danger-full-access":
		return 2, nil
	default:
		return 0, fmt.Errorf("unknown permission ceiling %q", value)
	}
}
func permissionAtMost(path, child, parent string) error {
	c, err := permissionRank(child)
	if err != nil {
		return fmt.Errorf("harness: %s: %w", path, err)
	}
	p, err := permissionRank(parent)
	if err != nil {
		return fmt.Errorf("harness: %s: %w", path, err)
	}
	if c > p {
		return fmt.Errorf("harness: %s permission %q escalates above %q", path, child, parent)
	}
	return nil
}
func effectSet(path string, values []string) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(values))
	for _, effect := range values {
		if effect == "" || strings.Contains(effect, "*") {
			return nil, fmt.Errorf("harness: %s contains invalid or wildcard effect %q", path, effect)
		}
		if !knownEffect(effect) {
			return nil, fmt.Errorf("harness: %s contains unknown effect %q", path, effect)
		}
		if _, exists := out[effect]; exists {
			return nil, fmt.Errorf("harness: %s contains duplicate effect %q", path, effect)
		}
		out[effect] = struct{}{}
	}
	return out, nil
}

func knownEffect(effect string) bool {
	switch effect {
	case "read", "write", "exec", "net", "destroy", "priv", "persist", "cred", "remote", "spend":
		return true
	default:
		return false
	}
}
func subsetEffects(path string, child, parent map[string]struct{}) error {
	for effect := range child {
		if _, ok := parent[effect]; !ok {
			return fmt.Errorf("harness: %s effect %q escalates beyond parent ceiling", path, effect)
		}
	}
	return nil
}
func budgetAtMost(path string, child, parent TokenBudget) error {
	if child.MaxInputTokens <= 0 || child.MaxOutputTokens <= 0 || child.MaxCostUSD <= 0 {
		return fmt.Errorf("harness: delegation %s requires positive budget limits", path)
	}
	if child.MaxInputTokens > parent.MaxInputTokens || child.MaxOutputTokens > parent.MaxOutputTokens || child.MaxCostUSD > parent.MaxCostUSD {
		return fmt.Errorf("harness: delegation %s budget exceeds route ceiling", path)
	}
	return nil
}
