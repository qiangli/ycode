package spec

import (
	"encoding/json"
	"reflect"
	"strings"
)

// JSONSchema returns the complete structural contract. Cross-reference,
// dependency and stage-state constraints remain compiler validations.
func JSONSchema() ([]byte, error) {
	builder := schemaBuilder{defs: make(map[string]any), working: make(map[string]bool)}
	root := builder.structSchema(reflect.TypeOf(Document{}))
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$id"] = "https://ycode.dev/schema/agent-v1alpha1.json"
	root["title"] = "Ycode Harness"
	root["$comment"] = "Property and type inventory. The ycode compiler is authoritative for required fields and cross-field validity."
	root["$defs"] = builder.defs
	properties := root["properties"].(map[string]any)
	properties["apiVersion"] = map[string]any{"const": APIVersion}
	properties["kind"] = map[string]any{"const": Kind}

	// Defaults templates use the same property contracts as their corresponding
	// sections/resources, but the template key set is the supported sections.
	specSchema := builder.defs["Spec"].(map[string]any)
	specProperties := specSchema["properties"].(map[string]any)
	defaults := make(map[string]any, len(defaultableSections))
	specType := reflect.TypeOf(Spec{})
	for i := range specType.NumField() {
		field := specType.Field(i)
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if _, allowed := defaultableSections[name]; !allowed {
			continue
		}
		templateType := field.Type
		if templateType.Kind() == reflect.Map {
			templateType = templateType.Elem()
		}
		defaults[name] = builder.schema(templateType)
	}
	specProperties["defaults"] = map[string]any{
		"type": "object", "additionalProperties": false, "properties": defaults,
	}
	return json.MarshalIndent(root, "", "  ")
}

type schemaBuilder struct {
	defs    map[string]any
	working map[string]bool
}

func (b *schemaBuilder) schema(t reflect.Type) any {
	if t.Kind() == reflect.Pointer {
		return b.schema(t.Elem())
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return true
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": b.schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": b.schema(t.Elem())}
	case reflect.Struct:
		name := t.Name()
		if name == "Document" {
			return b.structSchema(t)
		}
		if _, exists := b.defs[name]; !exists && !b.working[name] {
			b.working[name] = true
			b.defs[name] = b.structSchema(t)
			delete(b.working, name)
		}
		return map[string]any{"$ref": "#/$defs/" + name}
	default:
		return true
	}
}

func (b *schemaBuilder) structSchema(t reflect.Type) map[string]any {
	properties := make(map[string]any)
	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		properties[parts[0]] = b.schema(field.Type)
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
