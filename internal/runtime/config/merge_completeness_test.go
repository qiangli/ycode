package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A CONFIG FIELD THAT IS NOT MERGED IS A LIE.
//
// mergeFromFile is hand-written and field-by-field. A field added to Config but not added
// there parses cleanly, validates cleanly, appears in settings.json, and is SILENTLY
// DISCARDED. No error. No warning. The operator sets it, sees no complaint, and believes
// it took effect.
//
// It cost hours: maxToolIterations was set to 80 in settings.json and the agent was still
// cut off at 25 — twice — because the number never reached the code that reads it. And
// contextWindow / contextReserved shipped the same way: documented, announced, dead.
//
// This test walks every JSON-tagged scalar field on Config, writes a settings.json that
// sets ONLY that field to a distinctive value, loads it, and asserts the value arrived.
// It fails the build on the next unmerged field, which is the only way to stop the fourth
// one.
func TestEveryConfigFieldIsMerged(t *testing.T) {
	typ := reflect.TypeOf(Config{})

	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)

		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := tag
		for j, c := range tag {
			if c == ',' {
				name = tag[:j]
				break
			}
		}

		// Only scalars are checked here. Nested structs (Parallel, Chat, NATS, …) have
		// their own merge blocks and their own tests; a generic probe would have to
		// invent a valid value for each and would be more fragile than useful.
		var probe any
		switch f.Type.Kind() {
		case reflect.Int:
			probe = 4242
		case reflect.String:
			probe = "probe-value"
		default:
			continue
		}

		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")

			body, err := json.Marshal(map[string]any{name: probe})
			if err != nil {
				t.Fatalf("marshal probe: %v", err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatalf("write settings: %v", err)
			}

			cfg := &Config{}
			if err := mergeFromFile(cfg, path); err != nil {
				t.Fatalf("mergeFromFile: %v", err)
			}

			got := reflect.ValueOf(*cfg).Field(i).Interface()
			if !reflect.DeepEqual(got, probe) {
				t.Errorf("settings.json set %q = %v, but after merge Config.%s = %v.\n\n"+
					"This field is DECLARED BUT NOT MERGED. It parses, it validates, it is "+
					"silently discarded, and the operator is never told. Add it to "+
					"mergeFromFile.", name, probe, f.Name, got)
			}
		})
	}
}
