package agentdef

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OutputSchema defines the expected shape of an agent's response.
type OutputSchema struct {
	// JSONSchema is a JSON Schema definition for validating agent output.
	JSONSchema json.RawMessage `yaml:"json_schema,omitempty" json:"json_schema,omitempty"`

	// RequiredFields lists field names that must be present (lightweight check without full schema validation).
	RequiredFields []string `yaml:"required_fields,omitempty" json:"required_fields,omitempty"`
}

// OutputValidator validates agent output against a schema.
type OutputValidator interface {
	Validate(output string, schema *OutputSchema) (*ValidationResult, error)
}

// ValidationResult holds the outcome of output validation.
type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Parsed any      `json:"parsed,omitempty"`
	Errors []string `json:"errors,omitempty"`
	Raw    string   `json:"raw"`
}

// DefaultOutputValidator validates output against an OutputSchema using
// JSON Schema validation (if provided) and required field checks.
type DefaultOutputValidator struct{}

// Validate checks that output is valid JSON and optionally matches the schema.
func (v *DefaultOutputValidator) Validate(output string, schema *OutputSchema) (*ValidationResult, error) {
	if schema == nil {
		return &ValidationResult{Valid: true, Raw: output}, nil
	}

	result := &ValidationResult{Raw: output}

	// Extract JSON from the output (may be wrapped in markdown code fences).
	jsonStr := extractJSON(output)
	if jsonStr == "" {
		result.Errors = append(result.Errors, "output does not contain valid JSON")
		return result, nil
	}

	// Parse JSON.
	var parsed any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("invalid JSON: %v", err))
		return result, nil
	}
	result.Parsed = parsed

	// Check required fields if parsed is an object.
	if len(schema.RequiredFields) > 0 {
		obj, ok := parsed.(map[string]any)
		if !ok {
			result.Errors = append(result.Errors, "expected a JSON object")
			return result, nil
		}
		for _, field := range schema.RequiredFields {
			if _, exists := obj[field]; !exists {
				result.Errors = append(result.Errors, fmt.Sprintf("missing required field %q", field))
			}
		}
	}

	// Validate against JSON Schema if provided.
	if len(schema.JSONSchema) > 0 {
		if errs := validateJSONSchema(jsonStr, schema.JSONSchema); len(errs) > 0 {
			result.Errors = append(result.Errors, errs...)
		}
	}

	result.Valid = len(result.Errors) == 0
	return result, nil
}

// extractJSON extracts a JSON string from output that may contain markdown code fences.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)

	// Try raw JSON first.
	if (strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")) ||
		(strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]")) {
		return s
	}

	// Try extracting from markdown code fences.
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + len("```json")
		end := strings.Index(s[start:], "```")
		if end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + len("```")
		end := strings.Index(s[start:], "```")
		if end >= 0 {
			inner := strings.TrimSpace(s[start : start+end])
			if (strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}")) ||
				(strings.HasPrefix(inner, "[") && strings.HasSuffix(inner, "]")) {
				return inner
			}
		}
	}

	return ""
}

// validateJSONSchema validates a JSON string against a JSON Schema definition.
// Uses basic type/required checking without an external dependency.
func validateJSONSchema(jsonStr string, schema json.RawMessage) []string {
	var schemaDef map[string]any
	if err := json.Unmarshal(schema, &schemaDef); err != nil {
		return []string{fmt.Sprintf("invalid schema: %v", err)}
	}

	var data any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return []string{fmt.Sprintf("invalid JSON: %v", err)}
	}

	return validateValue(data, schemaDef, "")
}

// validateValue recursively validates a value against a schema definition.
func validateValue(data any, schema map[string]any, path string) []string {
	var errs []string

	// Check type.
	if expectedType, ok := schema["type"].(string); ok {
		if !matchesType(data, expectedType) {
			errs = append(errs, fmt.Sprintf("%s: expected type %q, got %T", pathStr(path), expectedType, data))
			return errs // stop on type mismatch
		}
	}

	// Object-specific checks.
	if obj, ok := data.(map[string]any); ok {
		// Check required fields.
		if required, ok := schema["required"].([]any); ok {
			for _, r := range required {
				field, _ := r.(string)
				if field != "" {
					if _, exists := obj[field]; !exists {
						errs = append(errs, fmt.Sprintf("%s: missing required field %q", pathStr(path), field))
					}
				}
			}
		}

		// Validate properties.
		if props, ok := schema["properties"].(map[string]any); ok {
			for key, propSchema := range props {
				if val, exists := obj[key]; exists {
					if ps, ok := propSchema.(map[string]any); ok {
						childPath := path + "." + key
						errs = append(errs, validateValue(val, ps, childPath)...)
					}
				}
			}
		}
	}

	// Array-specific checks.
	if arr, ok := data.([]any); ok {
		if items, ok := schema["items"].(map[string]any); ok {
			for i, item := range arr {
				childPath := fmt.Sprintf("%s[%d]", path, i)
				errs = append(errs, validateValue(item, items, childPath)...)
			}
		}
	}

	return errs
}

// matchesType checks if data matches the expected JSON Schema type.
func matchesType(data any, expectedType string) bool {
	switch expectedType {
	case "object":
		_, ok := data.(map[string]any)
		return ok
	case "array":
		_, ok := data.([]any)
		return ok
	case "string":
		_, ok := data.(string)
		return ok
	case "number", "integer":
		_, ok := data.(float64)
		return ok
	case "boolean":
		_, ok := data.(bool)
		return ok
	case "null":
		return data == nil
	}
	return true
}

// pathStr returns a human-readable path, defaulting to "$" for root.
func pathStr(path string) string {
	if path == "" {
		return "$"
	}
	return "$" + path
}

// FormatSchemaPrompt creates a prompt instruction for enforcing structured output.
func FormatSchemaPrompt(schema *OutputSchema) string {
	if schema == nil {
		return ""
	}

	var parts []string
	parts = append(parts, "You MUST respond with valid JSON matching the following constraints:")

	if len(schema.JSONSchema) > 0 {
		parts = append(parts, fmt.Sprintf("\nJSON Schema:\n```json\n%s\n```", string(schema.JSONSchema)))
	}

	if len(schema.RequiredFields) > 0 {
		parts = append(parts, fmt.Sprintf("\nRequired fields: %s", strings.Join(schema.RequiredFields, ", ")))
	}

	parts = append(parts, "\nDo not include any text outside the JSON object.")

	return strings.Join(parts, "\n")
}
