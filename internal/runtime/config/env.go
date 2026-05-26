package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// env.go — uniform `YCODE_<UPPER_SNAKE_PATH>` env-var overrides for
// every primitive leaf on the Config struct. Applied after the JSON
// tier merge in Loader.Load() (env overrides config file but is
// overridden by explicit CLI flags applied later in main.go).
//
// ============================================================================
// SAFEGUARDS — read before changing the naming convention or behavior
// ============================================================================
//
//  1. PREFIX IS LOAD-BEARING. `YCODE_` is the universal prefix. Don't
//     introduce variants (YCODE_CFG_, YCODE_RUNTIME_, etc.) — operators
//     should learn one rule, not three.
//
//  2. PATH DERIVATION IS A PUBLIC API. Once a deployment sets
//     YCODE_CONTAINER_SOCKET_PATH=/var/run/podman/podman.sock in their
//     CI pipeline, renaming the Go field SocketPath silently breaks it.
//     If you rename a Config field, add the old env var name to
//     legacyEnvAliases below for at least one release.
//
//  3. AUTO-ALLOCATE NESTED STRUCTS. When descending into a *Config
//     pointer that's nil, we allocate it so env-only deployments
//     (no settings.json on disk) can populate deep paths. The receivers
//     are designed to handle the freshly-allocated zero state — their
//     IsEnabled() methods return the same value for nil and zero-value
//     non-nil because they default-true via separate nil checks.
//
//  4. ONLY PRIMITIVES. Strings, ints, floats, bools, and pointer
//     variants thereof. Slices ([]string) and maps (map[string]X) are
//     intentionally skipped — env-var encoding of complex types
//     (comma-separated? JSON-encoded?) creates more bugs than it
//     solves. Set those via JSON.
//
//  5. THE Custom MAP IS EXEMPT. It accepts arbitrary plugin-defined
//     keys and resists schema enforcement by design.
//
//  6. PRECEDENCE FROM HIGHEST TO LOWEST:
//        CLI flags (cmd/ycode/main.go) > env vars (this file) >
//        local settings > project settings > per-project settings >
//        user settings > DefaultConfig().
//     If you add an env-var pass elsewhere, route it through here so
//     the precedence stays consistent.
//
// ============================================================================

// legacyEnvAliases maps deprecated env-var names to the canonical
// derived name for a Config field path. Operators using the old name
// still get correct behavior; the derived name is logged as the new
// canonical. Keep entries here for at least one release after a rename
// before dropping (announce in CHANGELOG).
//
// EMPTY today — added entries as a contract for future renames.
var legacyEnvAliases = map[string]string{
	// "OLD_NAME": "YCODE_NEW_NAME",
}

// ApplyEnvOverrides walks cfg's exported primitive leaves and applies
// any matching `YCODE_<UPPER_SNAKE_PATH>` environment variable. Returns
// the list of (envVar = configPath = value) records actually applied
// so callers can log or audit the override set.
//
// Auto-allocates nil *Config sub-structs on the way down so deep paths
// can be reached without a JSON layer present.
func ApplyEnvOverrides(cfg *Config) []EnvOverride {
	var out []EnvOverride
	if cfg == nil {
		return out
	}
	applyEnvToStruct(reflect.ValueOf(cfg).Elem(), "YCODE", "", &out)
	return out
}

// EnvOverride records one applied env-var → config-field mapping.
// Useful for operator inspection (`ycode config env`) and for
// telemetry that needs to attribute config divergence from disk.
type EnvOverride struct {
	EnvVar     string `json:"env_var"`
	ConfigPath string `json:"config_path"`
	Value      string `json:"value"`
}

func applyEnvToStruct(v reflect.Value, envPrefix, cfgPrefix string, out *[]EnvOverride) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		// Skip the freeform Custom map (safeguard #5) and any other
		// fields marked exempt via the `env:"-"` struct tag (no users
		// today; reserved for future opt-out).
		if tag := field.Tag.Get("env"); tag == "-" {
			continue
		}
		if field.Name == "Custom" {
			continue
		}

		envName := envPrefix + "_" + camelToScreamingSnake(field.Name)
		cfgPath := field.Name
		if cfgPrefix != "" {
			cfgPath = cfgPrefix + "." + field.Name
		}
		fv := v.Field(i)

		switch fv.Kind() {
		case reflect.Ptr:
			if fv.Type().Elem().Kind() == reflect.Struct {
				// Auto-allocate so env-only deployments work (#3).
				if fv.IsNil() {
					fv.Set(reflect.New(fv.Type().Elem()))
				}
				applyEnvToStruct(fv.Elem(), envName, cfgPath, out)
				continue
			}
			// Pointer to primitive (*bool, *float64): only assign on env hit.
			if raw, ok := lookupEnv(envName); ok {
				if assigned := assignPtrPrimitive(fv, raw); assigned {
					*out = append(*out, EnvOverride{EnvVar: envName, ConfigPath: cfgPath, Value: raw})
				}
			}

		case reflect.Struct:
			applyEnvToStruct(fv, envName, cfgPath, out)

		case reflect.Slice, reflect.Map, reflect.Interface, reflect.Chan, reflect.Func:
			// Safeguard #4 — primitives only.
			continue

		default:
			// Plain primitive (string, int*, uint*, float*, bool).
			if raw, ok := lookupEnv(envName); ok {
				if assigned := assignPrimitive(fv, raw); assigned {
					*out = append(*out, EnvOverride{EnvVar: envName, ConfigPath: cfgPath, Value: raw})
				}
			}
		}
	}
}

// lookupEnv resolves an env-var name including legacy aliases. The
// canonical name takes precedence — if both YCODE_FOO and OLD_FOO are
// set, YCODE_FOO wins. Empty values are treated as "not set" so an
// operator can unset a field by passing FOO= without exporting it.
func lookupEnv(envName string) (string, bool) {
	if v, ok := os.LookupEnv(envName); ok && v != "" {
		return v, true
	}
	// Legacy aliases (#2). Iterate the map — small enough.
	for old, canonical := range legacyEnvAliases {
		if canonical != envName {
			continue
		}
		if v, ok := os.LookupEnv(old); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

// assignPrimitive parses raw and writes into fv. Returns true on
// success. Silently no-ops on parse failure (bad env values shouldn't
// crash startup — they should be visible via Discover() or a future
// `ycode doctor` check). Future enhancement: return a parse error so
// callers can surface it.
func assignPrimitive(fv reflect.Value, raw string) bool {
	if !fv.CanSet() {
		return false
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
		return true
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return false
		}
		fv.SetBool(b)
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return false
		}
		fv.SetInt(n)
		return true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return false
		}
		fv.SetUint(n)
		return true
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return false
		}
		fv.SetFloat(f)
		return true
	}
	return false
}

// assignPtrPrimitive handles *bool, *float64, *int, *string by
// allocating the pointee and delegating to assignPrimitive.
func assignPtrPrimitive(fv reflect.Value, raw string) bool {
	elemType := fv.Type().Elem()
	if fv.IsNil() {
		fv.Set(reflect.New(elemType))
	}
	return assignPrimitive(fv.Elem(), raw)
}

// camelToScreamingSnake converts Go CamelCase to SCREAMING_SNAKE_CASE.
// Handles consecutive uppercase runs (acronyms) by treating them as a
// single token unless followed by a lowercase letter — e.g.
//
//	MaxTokens       → MAX_TOKENS
//	SampleRate      → SAMPLE_RATE
//	HTTPOnly        → HTTP_ONLY
//	OTLPGRPCPort    → OTLPGRPC_PORT     (note: see limitation below)
//	ProjectID       → PROJECT_ID
//
// Algorithm: insert an underscore before any uppercase letter that
// either (a) follows a lowercase letter or digit, or (b) is itself
// followed by a lowercase letter while preceded by an uppercase letter.
//
// LIMITATION: consecutive acronyms cannot be disambiguated without a
// dictionary. `OTLPGRPCPort` becomes `OTLPGRPC_PORT`, not
// `OTLP_GRPC_PORT`. Operators overriding such fields must use the
// derived name. Field names that combine multiple acronyms back-to-back
// are rare; rename them in Go to introduce a boundary (e.g.
// OtlpGrpcPort) if the env-var ergonomics become a problem.
func camelToScreamingSnake(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if i == 0 {
			b.WriteRune(r)
			continue
		}
		prev := runes[i-1]
		insert := false
		if isUpper(r) {
			if isLower(prev) || isDigit(prev) {
				insert = true
			} else if isUpper(prev) && i+1 < len(runes) && isLower(runes[i+1]) {
				insert = true
			}
		}
		if insert {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
func isLower(r rune) bool { return r >= 'a' && r <= 'z' }
func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// FormatOverrides renders the result of ApplyEnvOverrides as one line
// per applied var, suitable for `ycode doctor` output or a debug log.
func FormatOverrides(overrides []EnvOverride) string {
	if len(overrides) == 0 {
		return "(no env-var overrides applied)"
	}
	var b strings.Builder
	for _, o := range overrides {
		fmt.Fprintf(&b, "%s = %s  (-> %s)\n", o.EnvVar, o.Value, o.ConfigPath)
	}
	return b.String()
}
