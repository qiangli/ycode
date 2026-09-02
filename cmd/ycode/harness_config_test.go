package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessValidateCommand(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	cmd := newHarnessValidateCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--file", filepath.Join("..", "..", "examples", "agent.yaml")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "valid: ycode (1 agents, 1 pipelines)") {
		t.Fatalf("output = %q", got)
	}
}

func TestHarnessSchemaCommand(t *testing.T) {
	cmd := newHarnessSchemaCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(output.Bytes(), &schema); err != nil {
		t.Fatal(err)
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %#v", schema["additionalProperties"])
	}
}
