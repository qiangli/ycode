package ycodecli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessValidateCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BASHY_KB_DIR", filepath.Join(dir, "kb"))
	t.Setenv("BASHY_HOME", filepath.Join(dir, "bashy-home"))
	t.Setenv("BASHY_SKILLS_DIR", filepath.Join(dir, "skills"))
	t.Setenv("YCODE_DATA_DIR", filepath.Join(dir, "ycode-data"))
	t.Setenv("OPENAI_API_KEY", "test-secret")
	cmd := testRoot(t)
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"validate", "--file", filepath.Join("..", "..", "examples", "agent.yaml")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "valid: ycode (2 agents, 23 pipelines)") {
		t.Fatalf("output = %q", got)
	}
}

func TestHarnessSchemaCommand(t *testing.T) {
	cmd := testRoot(t)
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"schema"})
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
