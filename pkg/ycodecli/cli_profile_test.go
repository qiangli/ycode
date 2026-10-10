package ycodecli

import (
	"errors"
	"os"
	"testing"

	"github.com/qiangli/ycode/examples"
	"github.com/qiangli/ycode/internal/harness/spec"
	"gopkg.in/yaml.v3"
)

func TestCLIProfileSelection(t *testing.T) {
	isolatedCLICwd(t)
	var seed spec.Document
	if err := yaml.Unmarshal(examples.Agent(), &seed); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args     []string
		want     string
		explicit bool
	}{
		{[]string{"--yolo"}, "agent-yolo.yaml", true},
		{[]string{"--yolo=true", "validate"}, "agent-yolo.yaml", true},
		{[]string{"--yolo=false"}, "agent.yaml", false},
		{[]string{"--yolo", "--yolo=false"}, "agent.yaml", false},
		{[]string{"prompt", "--", "--yolo"}, "agent.yaml", false},
		{[]string{"shell", "-c", "--yolo"}, "agent.yaml", false},
	} {
		path, explicit, err := selectedConfig(seed, tc.args)
		if err != nil || path != tc.want || explicit != tc.explicit {
			t.Errorf("%q: path %q explicit %v err %v", tc.args, path, explicit, err)
		}
	}
	for _, args := range [][]string{{"--file", "custom.yaml", "--yolo"}, {"--yolo", "--file=custom.yaml"}, {"--yolo=invalid"}} {
		if _, _, err := selectedConfig(seed, args); err == nil {
			t.Errorf("accepted conflicting/invalid args %q", args)
		}
	}
	t.Setenv("YCODE_CONFIG", "custom.yaml")
	if _, _, err := selectedConfig(seed, []string{"--yolo"}); err == nil {
		t.Fatal("environment conflict accepted")
	}
	t.Setenv("YCODE_CONFIG", "")
	if _, err := discoverCLI([]string{"--yolo"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing profile should fail closed: %v", err)
	}
}
